package gh

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/fakegh"
)

func TestResolvePointerRelease_NoPointerAssetPassesThrough(t *testing.T) {
	rel := Release{TagName: "b1234", Assets: []Asset{{Name: "llama-b1234-bin-linux-x64.tar.gz"}}}
	got, err := resolvePointerRelease(context.Background(), "ggml-org", "llama.cpp", rel)
	if err != nil {
		t.Fatal(err)
	}
	if got.TagName != rel.TagName || len(got.Assets) != 1 {
		t.Errorf("expected the release to pass through unchanged, got %+v", got)
	}
}

// TestGetLatestRelease_PointerHop covers llama.cpp's nightly-tag.txt
// convention end to end: a "v0.2.0" release carrying only that asset gets
// its Assets replaced with the b1234 release's real binaries, while
// TagName stays v0.2.0.
func TestGetLatestRelease_PointerHop(t *testing.T) {
	s := fakeAPI(t)
	s.AddRelease("ggml-org/llama.cpp", fakegh.Release{Tag: "b1234", Prerelease: true, Assets: []fakegh.Asset{{Name: "llama-b1234-bin-linux-x64.tar.gz", Body: []byte("bin")}}})
	s.AddRelease("ggml-org/llama.cpp", fakegh.Release{Tag: "v0.2.0", Assets: []fakegh.Asset{{Name: "nightly-tag.txt", Body: []byte("b1234\n")}}})

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
	if rel.DownloadTag() != "b1234" {
		t.Errorf("expected assets to be downloaded from b1234, got %s", rel.DownloadTag())
	}
}

func TestGetLatestRelease_PointerHop_DownloadFails(t *testing.T) {
	s := fakeAPI(t)
	s.AddRelease("ggml-org/llama.cpp", fakegh.Release{Tag: "v0.2.0", Assets: []fakegh.Asset{{Name: "nightly-tag.txt", Body: []byte("b1234")}}})
	s.Fail = func(r *http.Request) (int, string) {
		if strings.Contains(r.URL.Path, "/releases/assets/") {
			return 502, "network error"
		}
		return 0, ""
	}

	if _, err := GetLatestRelease(context.Background(), "ggml-org", "llama.cpp"); err == nil {
		t.Fatal("expected an error when the pointer asset can't be fetched")
	}
}
