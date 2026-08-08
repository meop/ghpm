package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/store"
)

// fakeKebab stages a kebab script that records its args to a file alongside
// it, then exits 0 — enough for shim.Create to succeed without a real sheesh
// install.
func fakeKebab(t *testing.T) {
	t.Helper()
	shimDir, err := store.ShimDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(shimDir, 0755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		script := "@echo off\necho %* >> \"%~dp0kebab-args.txt\"\n"
		if err := os.WriteFile(filepath.Join(shimDir, "kebab.bat"), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	} else {
		script := "#!/bin/sh\necho \"$@\" >> \"$(dirname \"$0\")/kebab-args.txt\"\n"
		if err := os.WriteFile(filepath.Join(shimDir, "kebab"), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
}

func names(assets []gh.Asset) []string {
	out := make([]string, len(assets))
	for i, a := range assets {
		out[i] = a.Name
	}
	return out
}

func TestResolvePriorAssets_CleanCarryOver(t *testing.T) {
	// Both stored assets resolve to a unique, distinct asset in the bumped
	// release, so the selection carries over with no prompt.
	newAssets := []gh.Asset{
		{Name: "foo-2.0-linux.tar.gz", Size: 100},
		{Name: "bar-2.0-linux.tar.gz", Size: 100},
		{Name: "foo-2.0-darwin.tar.gz", Size: 100},
	}
	old := []string{"foo-1.0-linux.tar.gz", "bar-1.0-linux.tar.gz"}
	chosens, clean := resolvePriorAssets(newAssets, old)
	if !clean {
		t.Fatalf("expected clean resolution, got clean=false")
	}
	got := names(chosens)
	if len(got) != 2 || got[0] != "foo-2.0-linux.tar.gz" || got[1] != "bar-2.0-linux.tar.gz" {
		t.Errorf("unexpected chosens: %v", got)
	}
}

func TestResolvePriorAssets_AmbiguousSplit_NotClean(t *testing.T) {
	// The release now ships two variants that the stored name matches equally
	// (the cuda-version case), so resolution is ambiguous and the whole package
	// must fall back to a fresh prompt.
	newAssets := []gh.Asset{
		{Name: "tool-cuda-12.4.tar.gz", Size: 100},
		{Name: "tool-cuda-13.3.tar.gz", Size: 100},
	}
	old := []string{"tool-cuda-13.3.tar.gz"}
	if _, clean := resolvePriorAssets(newAssets, old); clean {
		t.Errorf("expected clean=false for ambiguous match")
	}
}

func TestResolvePriorAssets_Collision_NotClean(t *testing.T) {
	// Two stored assets collapse onto the same new asset, so the count can't be
	// preserved — not clean.
	newAssets := []gh.Asset{
		{Name: "foo-2.0-linux.tar.gz", Size: 100},
	}
	old := []string{"foo-1.0-linux.tar.gz", "foo-1.0-linux.tar.gz"}
	if _, clean := resolvePriorAssets(newAssets, old); clean {
		t.Errorf("expected clean=false when two stored assets collide on one")
	}
}

func TestResolvePriorAssets_Missing_NotClean(t *testing.T) {
	// The stored asset no longer exists in the release; hint-only resolution
	// reports not-clean rather than guessing a platform asset.
	newAssets := []gh.Asset{
		{Name: "other-2.0-linux.tar.gz", Size: 100},
	}
	old := []string{"foo-1.0-linux.tar.gz"}
	if _, clean := resolvePriorAssets(newAssets, old); clean {
		t.Errorf("expected clean=false when stored asset is gone")
	}
}

func TestResolvePriorAssets_NoPriorAssets_NotClean(t *testing.T) {
	if _, clean := resolvePriorAssets(nil, nil); clean {
		t.Errorf("expected clean=false with no prior assets")
	}
}

// TestSyncBinShims_RemovesStaleWhenAllBinsGone guards the sync.go regression
// where a release dropping every bin a package used to have (newBin empty)
// left the old shim dangling on disk because shim removal was only reached
// when newBin was non-empty.
func TestSyncBinShims_RemovesStaleWhenAllBinsGone(t *testing.T) {
	withHome(t)
	fakeKebab(t)
	binDir, err := store.BinDir()
	if err != nil {
		t.Fatal(err)
	}
	stalePath := filepath.Join(binDir, "old")
	if err := os.WriteFile(stalePath, []byte{}, 0755); err != nil {
		t.Fatal(err)
	}

	errs := syncBinShims(t.TempDir(), map[string]string{"old": "bin/old"}, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Errorf("expected stale shim %s to be removed, stat err = %v", stalePath, err)
	}
}

func TestSyncBinShims_RemovesOldAndCreatesNew(t *testing.T) {
	withHome(t)
	fakeKebab(t)
	binDir, err := store.BinDir()
	if err != nil {
		t.Fatal(err)
	}
	stalePath := filepath.Join(binDir, "old")
	if err := os.WriteFile(stalePath, []byte{}, 0755); err != nil {
		t.Fatal(err)
	}

	errs := syncBinShims(t.TempDir(), map[string]string{"old": "bin/old"}, map[string]string{"new": "bin/new"})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Errorf("expected old shim to be removed")
	}
	shimDir, err := store.ShimDir()
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(filepath.Join(shimDir, "kebab-args.txt"))
	if err != nil {
		t.Fatalf("expected kebab to have been invoked: %v", err)
	}
	if !strings.Contains(string(args), "new") {
		t.Errorf("expected kebab args to reference the new shim name: %s", args)
	}
}

// TestSyncPkgFonts_UninstallsStaleWhenAllFontsGone guards the sync.go
// regression mirroring the bin one above: a release dropping every font a
// package used to have (newFont empty) left the old font file installed
// because uninstall was only reached when newFont was non-empty.
func TestSyncPkgFonts_UninstallsStaleWhenAllFontsGone(t *testing.T) {
	home := withHome(t)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg-data"))
	fontsDir := filepath.Join(home, "xdg-data", "fonts")
	makeFontFile(t, fontsDir, "Hack-Regular.ttf")

	errs, err := syncPkgFonts(t.TempDir(), map[string]string{"hack": "Hack-Regular.ttf"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected font errors: %v", errs)
	}
	if fontInstalled("Hack-Regular.ttf", fontsDir) {
		t.Errorf("expected stale font to be uninstalled")
	}
}

func TestSyncPkgFonts_NoOpWhenBothEmpty(t *testing.T) {
	withHome(t)
	wantDir, err := userFontDir()
	if err != nil {
		t.Fatal(err)
	}

	errs, err := syncPkgFonts(t.TempDir(), nil, nil)
	if err != nil || len(errs) != 0 {
		t.Fatalf("expected a clean no-op, got errs=%v err=%v", errs, err)
	}
	if _, statErr := os.Stat(wantDir); !os.IsNotExist(statErr) {
		t.Errorf("expected fonts dir to never be created when both maps are empty")
	}
}
