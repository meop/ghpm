package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/ui"
)

// fakeGHClient implements gh.Client in-process, so downloadAllAssets can be
// unit tested without shelling out to a fake gh binary.
type fakeGHClient struct {
	mu         sync.Mutex
	downloaded []string
	failAssets map[string]bool
}

func (f *fakeGHClient) GetLatestRelease(context.Context, string, string) (gh.Release, error) {
	return gh.Release{}, nil
}
func (f *fakeGHClient) GetReleaseByTag(context.Context, string, string, string) (gh.Release, error) {
	return gh.Release{}, nil
}
func (f *fakeGHClient) FindLatestMatching(context.Context, string, string, config.Constraint) (gh.Release, error) {
	return gh.Release{}, nil
}
func (f *fakeGHClient) ListReleases(context.Context, string, string) ([]gh.Release, error) {
	return nil, nil
}
func (f *fakeGHClient) DownloadAsset(_ context.Context, _, _, _, pattern, _ string) error {
	f.mu.Lock()
	f.downloaded = append(f.downloaded, pattern)
	fail := f.failAssets[pattern]
	f.mu.Unlock()
	if fail {
		return fmt.Errorf("simulated failure for %s", pattern)
	}
	return nil
}
func (f *fakeGHClient) BatchLatestVersions(context.Context, []gh.BatchItem, string) []gh.BatchResult {
	return nil
}

func TestDownloadAllAssets_FlattensAcrossPackages(t *testing.T) {
	client := &fakeGHClient{}
	downloads := []assetDownload{
		{pkgIdx: 0, owner: "o", repo: "r1", tagName: "v1", cacheDir: t.TempDir(), displayName: "pkg1", asset: gh.Asset{Name: "a1"}},
		{pkgIdx: 0, owner: "o", repo: "r1", tagName: "v1", cacheDir: t.TempDir(), displayName: "pkg1", asset: gh.Asset{Name: "a2"}},
		{pkgIdx: 1, owner: "o", repo: "r2", tagName: "v1", cacheDir: t.TempDir(), displayName: "pkg2", asset: gh.Asset{Name: "b1"}},
	}
	quiet = true
	defer func() { quiet = false }()

	errs := downloadAllAssets(context.Background(), client, downloads, 5)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(client.downloaded) != 3 {
		t.Fatalf("expected all 3 assets across both packages to be attempted in one flat pool, got %d: %v", len(client.downloaded), client.downloaded)
	}
}

func TestDownloadAllAssets_SkipsAlreadyCached(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cached.tar.gz"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	client := &fakeGHClient{}
	downloads := []assetDownload{
		{pkgIdx: 0, owner: "o", repo: "r", tagName: "v1", cacheDir: dir, displayName: "pkg", asset: gh.Asset{Name: "cached.tar.gz"}},
	}

	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	errs := downloadAllAssets(context.Background(), client, downloads, 5)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(client.downloaded) != 0 {
		t.Errorf("expected the already-cached asset to be skipped, but DownloadAsset was called: %v", client.downloaded)
	}

	out := buf.String()
	if !strings.Contains(out, "pkg: asset found [cached.tar.gz]") {
		t.Errorf("expected a cached-asset report line, got output:\n%s", out)
	}
	if strings.Contains(out, "downloading") {
		t.Errorf("a cached asset must not also print a downloading line, got:\n%s", out)
	}
}

// TestDownloadAllAssets_TrailingSepBeforeNextOutput is the regression test for
// the reported spacing bug: the download block ran tight into whatever the
// caller printed next (e.g. "bin found" report lines or a summary line), with
// no blank line between the two sections. downloadAllAssets must request a
// blank line (via sep/ui.Break) after it finishes, so the next real print gets
// separated — but only when something actually follows, never as a trailing
// blank if the download block turns out to be the last output.
func TestDownloadAllAssets_TrailingSepBeforeNextOutput(t *testing.T) {
	t.Run("real download, then more output", func(t *testing.T) {
		var buf bytes.Buffer
		ui.SetOutput(&buf)
		t.Cleanup(func() { ui.SetOutput(os.Stdout) })

		client := &fakeGHClient{}
		downloads := []assetDownload{
			{pkgIdx: 0, owner: "o", repo: "r", tagName: "v1", cacheDir: t.TempDir(), displayName: "pkg", asset: gh.Asset{Name: "a1"}},
		}
		if errs := downloadAllAssets(context.Background(), client, downloads, 5); len(errs) != 0 {
			t.Fatalf("expected no errors, got %v", errs)
		}
		print("next section")

		want := "pkg: asset downloading [a1]...\n\nnext section\n"
		if got := buf.String(); got != want {
			t.Errorf("got %q, want %q (blank line between the download block and the next section)", got, want)
		}
	})

	t.Run("cached-only, then more output", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "cached.tar.gz"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		ui.SetOutput(&buf)
		t.Cleanup(func() { ui.SetOutput(os.Stdout) })

		client := &fakeGHClient{}
		downloads := []assetDownload{
			{pkgIdx: 0, owner: "o", repo: "r", tagName: "v1", cacheDir: dir, displayName: "pkg", asset: gh.Asset{Name: "cached.tar.gz"}},
		}
		if errs := downloadAllAssets(context.Background(), client, downloads, 5); len(errs) != 0 {
			t.Fatalf("expected no errors, got %v", errs)
		}
		print("next section")

		want := "pkg: asset found [cached.tar.gz]\n\nnext section\n"
		if got := buf.String(); got != want {
			t.Errorf("got %q, want %q (blank line between the cached report and the next section)", got, want)
		}
	})

	t.Run("nothing follows, no trailing blank", func(t *testing.T) {
		var buf bytes.Buffer
		ui.SetOutput(&buf)
		t.Cleanup(func() { ui.SetOutput(os.Stdout) })

		client := &fakeGHClient{}
		downloads := []assetDownload{
			{pkgIdx: 0, owner: "o", repo: "r", tagName: "v1", cacheDir: t.TempDir(), displayName: "pkg", asset: gh.Asset{Name: "a1"}},
		}
		if errs := downloadAllAssets(context.Background(), client, downloads, 5); len(errs) != 0 {
			t.Fatalf("expected no errors, got %v", errs)
		}

		want := "pkg: asset downloading [a1]...\n"
		if got := buf.String(); got != want {
			t.Errorf("got %q, want %q (no trailing blank when the download block is the last output)", got, want)
		}
	})
}

func TestDownloadAllAssets_ErrorsMapToOwningPackage(t *testing.T) {
	client := &fakeGHClient{failAssets: map[string]bool{"bad1": true, "bad2": true}}
	downloads := []assetDownload{
		{pkgIdx: 0, owner: "o", repo: "r", tagName: "v1", cacheDir: t.TempDir(), displayName: "pkg0", asset: gh.Asset{Name: "bad1"}},
		{pkgIdx: 0, owner: "o", repo: "r", tagName: "v1", cacheDir: t.TempDir(), displayName: "pkg0", asset: gh.Asset{Name: "bad2"}},
		{pkgIdx: 1, owner: "o", repo: "r", tagName: "v1", cacheDir: t.TempDir(), displayName: "pkg1", asset: gh.Asset{Name: "ok"}},
	}
	quiet = true
	defer func() { quiet = false }()

	errs := downloadAllAssets(context.Background(), client, downloads, 5)
	if len(errs) != 1 {
		t.Fatalf("expected exactly one failed package, got %d: %v", len(errs), errs)
	}
	if _, ok := errs[0]; !ok {
		t.Errorf("expected pkgIdx 0 to have an error, got %v", errs)
	}
	if err, ok := errs[1]; ok {
		t.Errorf("pkgIdx 1 should have no error, got %v", err)
	}
	if !strings.Contains(errs[0].Error(), "bad1") {
		t.Errorf("expected the first failing asset (bad1, by input order) to be the representative error, got: %v", errs[0])
	}
}
