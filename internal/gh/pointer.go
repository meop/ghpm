package gh

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// pointerHop resolves rel, which may only name (not carry) its real assets,
// to the release whose Assets should actually be used.
type pointerHop func(ctx context.Context, owner, repo string, rel Release) (Release, error)

// pointerHops holds the one-off, per-repo knowledge needed to follow a
// release's pointer to its real assets. A repo lands here when a single
// release can't be trusted to carry its own binaries — llama.cpp cuts a
// slow-moving "stable" release that instead names the fast-moving build tag
// that has them, so every layer above this one can keep assuming a Release's
// own Assets are the ones to install. Add an entry here for the next repo
// that does something similar, rather than teaching that repo's asset shape
// to callers.
var pointerHops = map[string]pointerHop{
	"ggml-org/llama.cpp": llamaCppPointerHop,
}

// resolvePointerRelease is the single choke point every Release passes
// through before a caller sees it (see GetLatestRelease and getReleaseView).
// rel's own TagName is always preserved — it's what ghpm records as the
// installed version and what later sync/outdated comparisons key off — only
// its Assets are ever replaced, with the pointed-to release's.
func resolvePointerRelease(ctx context.Context, owner, repo string, rel Release) (Release, error) {
	hop, ok := pointerHops[owner+"/"+repo]
	if !ok {
		return rel, nil
	}
	hopped, err := hop(ctx, owner, repo, rel)
	if err != nil {
		return Release{}, err
	}
	hopped.TagName = rel.TagName
	return hopped, nil
}

// llamaCppPointerHop follows llama.cpp's "nightly-tag.txt" convention: a
// stable vX.Y.Z release carries exactly that one asset in place of real
// binaries, and its content is the b<build> tag of the nightly release that
// actually has them (see the "Web UI" note in a stable release's own body,
// e.g. https://github.com/ggml-org/llama.cpp/releases/tag/v0.2.0). A release
// without that asset already carries its own, so it's returned unchanged —
// the pointer is not guaranteed to stick around forever, or to be there from
// the very first stable release a user happens to land on.
func llamaCppPointerHop(ctx context.Context, owner, repo string, rel Release) (Release, error) {
	const pointerAsset = "nightly-tag.txt"

	var found bool
	for _, a := range rel.Assets {
		if a.Name == pointerAsset {
			found = true
			break
		}
	}
	if !found {
		return rel, nil
	}

	tmp, err := os.MkdirTemp("", "ghpm-pointer-*")
	if err != nil {
		return Release{}, err
	}
	defer os.RemoveAll(tmp)

	if err := DownloadAsset(ctx, owner, repo, rel.TagName, pointerAsset, tmp); err != nil {
		return Release{}, fmt.Errorf("release %s: fetching %s: %w", rel.TagName, pointerAsset, err)
	}
	body, err := os.ReadFile(filepath.Join(tmp, pointerAsset))
	if err != nil {
		return Release{}, fmt.Errorf("release %s: reading %s: %w", rel.TagName, pointerAsset, err)
	}
	tag := strings.TrimSpace(string(body))
	if tag == "" {
		return Release{}, fmt.Errorf("release %s: %s was empty", rel.TagName, pointerAsset)
	}

	hopped, err := viewRelease(ctx, owner, repo, tag)
	if err != nil {
		return Release{}, fmt.Errorf("release %s points at %q, which could not be fetched: %w", rel.TagName, tag, err)
	}
	return hopped, nil
}
