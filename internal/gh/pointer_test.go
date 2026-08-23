package gh

import (
	"context"
	"testing"
)

func TestResolvePointerRelease_UnknownRepoPassesThrough(t *testing.T) {
	rel := Release{TagName: "v1.0.0", Assets: []Asset{{Name: "fzf-linux-amd64.tar.gz"}}}
	got, err := resolvePointerRelease(context.Background(), "junegunn", "fzf", rel)
	if err != nil {
		t.Fatal(err)
	}
	if got.TagName != rel.TagName || len(got.Assets) != 1 {
		t.Errorf("expected an unlisted repo's release to pass through unchanged, got %+v", got)
	}
}

// TestLlamaCppPointerHop_NoPointerAssetPassesThrough covers an ordinary
// "b<n>" release, which already carries its own binaries and has no
// nightly-tag.txt asset to follow.
func TestLlamaCppPointerHop_NoPointerAssetPassesThrough(t *testing.T) {
	rel := Release{TagName: "b1234", Assets: []Asset{{Name: "llama-b1234-bin-linux-x64.tar.gz"}}}
	got, err := llamaCppPointerHop(context.Background(), "ggml-org", "llama.cpp", rel)
	if err != nil {
		t.Fatal(err)
	}
	if got.TagName != rel.TagName || len(got.Assets) != 1 {
		t.Errorf("expected the release to pass through unchanged, got %+v", got)
	}
}

// TestGetLatestRelease_LlamaCppPointerHop covers the full hop end to end: a
// "v0.2.0" release carrying only nightly-tag.txt gets its Assets replaced
// with the b1234 release's real binaries, while TagName stays v0.2.0 — that's
// what ghpm records as the installed version, and it must keep meaning
// "stable channel", not the nightly build it happened to resolve to today.
func TestGetLatestRelease_LlamaCppPointerHop(t *testing.T) {
	fakeGH(t, `
		if [ "$1 $2" = "release view" ]; then
			if [ "$3" = "-R" ]; then
				echo '{"tagName":"v0.2.0","assets":[{"name":"nightly-tag.txt","size":10,"url":"x"}]}'
				exit 0
			fi
			if [ "$3" = "b1234" ]; then
				echo '{"tagName":"b1234","assets":[{"name":"llama-b1234-bin-linux-x64.tar.gz","size":999,"url":"y"}]}'
				exit 0
			fi
		fi
		if [ "$1 $2" = "release download" ]; then
			dest=""
			prev=""
			for a in "$@"; do
				if [ "$prev" = "-D" ]; then dest="$a"; fi
				prev="$a"
			done
			printf 'b1234' > "$dest/nightly-tag.txt"
			exit 0
		fi
		echo "unexpected gh invocation: $*" >&2
		exit 1
	`)

	rel, err := GetLatestRelease(context.Background(), "ggml-org", "llama.cpp")
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "v0.2.0" {
		t.Errorf("expected the caller-facing tag to stay v0.2.0, got %s", rel.TagName)
	}
	if len(rel.Assets) != 1 || rel.Assets[0].Name != "llama-b1234-bin-linux-x64.tar.gz" {
		t.Errorf("expected the hopped-to release's real asset, got %v", rel.Assets)
	}
}

// TestGetLatestRelease_LlamaCppPointerHop_DownloadFails confirms a failure to
// fetch the pointer asset surfaces as an error rather than a release with no
// usable assets.
func TestGetLatestRelease_LlamaCppPointerHop_DownloadFails(t *testing.T) {
	fakeGH(t, `
		if [ "$1 $2" = "release view" ] && [ "$3" = "-R" ]; then
			echo '{"tagName":"v0.2.0","assets":[{"name":"nightly-tag.txt","size":10,"url":"x"}]}'
			exit 0
		fi
		if [ "$1 $2" = "release download" ]; then
			echo "network error" >&2
			exit 1
		fi
		exit 1
	`)

	_, err := GetLatestRelease(context.Background(), "ggml-org", "llama.cpp")
	if err == nil {
		t.Fatal("expected an error when the pointer asset can't be fetched")
	}
}
