package gh

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// pointerAssetNames are asset filenames known to hold a tag rather than a
// binary: a release carrying one of these has its real assets under the tag
// named in the file's content, not attached to this release itself.
// llama.cpp's stable releases use nightly-tag.txt this way. Add a name here
// for the next repo that does the same, pointing at whatever tag actually
// has the assets.
var pointerAssetNames = []string{
	"nightly-tag.txt",
}

// resolvePointerRelease is the single choke point every Release passes
// through before a caller sees it (see GetLatestRelease and getReleaseView).
// rel's own TagName is preserved; only its Assets are replaced, with the
// pointed-to release's, when a pointer asset is present.
func resolvePointerRelease(ctx context.Context, owner, repo string, rel Release) (Release, error) {
	var pointer string
	for _, a := range rel.Assets {
		if slices.Contains(pointerAssetNames, a.Name) {
			pointer = a.Name
			break
		}
	}
	if pointer == "" {
		return rel, nil
	}

	tmp, err := os.MkdirTemp("", "ghpm-pointer-*")
	if err != nil {
		return Release{}, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	if err := DownloadAsset(ctx, owner, repo, rel.TagName, pointer, tmp); err != nil {
		return Release{}, fmt.Errorf("release %s: fetching %s: %w", rel.TagName, pointer, err)
	}
	body, err := os.ReadFile(filepath.Join(tmp, pointer))
	if err != nil {
		return Release{}, fmt.Errorf("release %s: reading %s: %w", rel.TagName, pointer, err)
	}
	tag := strings.TrimSpace(string(body))
	if tag == "" {
		return Release{}, fmt.Errorf("release %s: %s was empty", rel.TagName, pointer)
	}

	hopped, err := viewRelease(ctx, owner, repo, tag)
	if err != nil {
		return Release{}, fmt.Errorf("release %s points at %q, which could not be fetched: %w", rel.TagName, tag, err)
	}
	hopped.TagName = rel.TagName
	return hopped, nil
}
