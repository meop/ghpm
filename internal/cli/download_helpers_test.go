package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
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
	quiet = true
	defer func() { quiet = false }()

	errs := downloadAllAssets(context.Background(), client, downloads, 5)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(client.downloaded) != 0 {
		t.Errorf("expected the already-cached asset to be skipped, but DownloadAsset was called: %v", client.downloaded)
	}
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
