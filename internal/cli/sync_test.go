package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/faketool"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/store"
)

// fakeMallet stages a mallet script that records its args to a file alongside
// it, then exits 0 — enough for shim.Create to succeed without a real wedge
// install.
func fakeMallet(t *testing.T) {
	t.Helper()
	shimDir, err := store.ShimDir()
	if err != nil {
		t.Fatal(err)
	}
	faketool.Write(t, filepath.Join(shimDir, exeName("mallet")), `echo "$@" >> "$(dirname "$0")/mallet-args.txt"`)
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
	chosens, clean := resolvePriorAssets(newAssets, old, "2.0")
	if !clean {
		t.Fatalf("expected clean resolution, got clean=false")
	}
	got := names(chosens)
	if len(got) != 2 || got[0] != "foo-2.0-linux.tar.gz" || got[1] != "bar-2.0-linux.tar.gz" {
		t.Errorf("unexpected chosens: %v", got)
	}
}

// TestResolvePriorAssets_BuildNumberBump_CleanCarryOver guards the reported
// llama.cpp regression: its release assets embed a "b<n>" build number
// (isVersionToken never recognized "b1234" as version-shaped, since it only
// strips a leading "v"), so every sync compared it for exact token equality,
// never matched, and fell back to a full asset reprompt on every run even
// though nothing the user picked had actually changed.
func TestResolvePriorAssets_BuildNumberBump_CleanCarryOver(t *testing.T) {
	newAssets := []gh.Asset{
		{Name: "llama-b2345-bin-ubuntu-x64.zip", Size: 100},
		{Name: "llama-b2345-bin-macos-arm64.zip", Size: 100},
	}
	old := []string{"llama-b2300-bin-ubuntu-x64.zip"}
	chosens, clean := resolvePriorAssets(newAssets, old, "2345")
	if !clean {
		t.Fatalf("expected clean resolution, got clean=false")
	}
	got := names(chosens)
	if len(got) != 1 || got[0] != "llama-b2345-bin-ubuntu-x64.zip" {
		t.Errorf("unexpected chosens: %v", got)
	}
}

func TestResolvePriorAssets_AmbiguousSplit_NotClean(t *testing.T) {
	// The release now ships two variants that the stored name matches equally
	// (the cuda-version case: one is a byte-identical carryover, the other a
	// valid bump to the release's own version 12.4), so resolution is
	// ambiguous and the whole package must fall back to a fresh prompt.
	newAssets := []gh.Asset{
		{Name: "tool-cuda-12.4.tar.gz", Size: 100},
		{Name: "tool-cuda-13.3.tar.gz", Size: 100},
	}
	old := []string{"tool-cuda-13.3.tar.gz"}
	if _, clean := resolvePriorAssets(newAssets, old, "12.4"); clean {
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
	if _, clean := resolvePriorAssets(newAssets, old, "2.0"); clean {
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
	if _, clean := resolvePriorAssets(newAssets, old, "2.0"); clean {
		t.Errorf("expected clean=false when stored asset is gone")
	}
}

// TestResolvePriorAssets_VersionMismatch_NotClean guards a packaging
// inconsistency: the differing token looks like a version bump in shape, but
// its embedded version isn't actually the release being installed (e.g. an
// asset that wasn't updated to match its own release tag). That must not be
// silently accepted as a carryover.
func TestResolvePriorAssets_VersionMismatch_NotClean(t *testing.T) {
	newAssets := []gh.Asset{
		{Name: "bun-v2.3.4.6-bun.tar.gz", Size: 100},
	}
	old := []string{"bun-v1.2.3.4-bun.tar.gz"}
	if _, clean := resolvePriorAssets(newAssets, old, "2.3.4.5"); clean {
		t.Errorf("expected clean=false when the asset's version doesn't match the release's own version")
	}
}

func TestResolvePriorAssets_NoPriorAssets_NotClean(t *testing.T) {
	if _, clean := resolvePriorAssets(nil, nil, ""); clean {
		t.Errorf("expected clean=false with no prior assets")
	}
}

// TestSyncBinShims_RemovesStaleWhenAllBinsGone guards the sync.go regression
// where a release dropping every bin a package used to have (newBin empty)
// left the old shim dangling on disk because shim removal was only reached
// when newBin was non-empty.
func TestSyncBinShims_RemovesStaleWhenAllBinsGone(t *testing.T) {
	withHome(t)
	fakeMallet(t)
	binDir, err := store.BinDir()
	if err != nil {
		t.Fatal(err)
	}
	stalePath := filepath.Join(binDir, "old")
	writeFakeShim(t, stalePath)

	installed, errs := syncBinShims(&config.Settings{}, &journal{}, t.TempDir(), map[string]string{"old": "bin/old"}, nil, false)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(installed) != 0 {
		t.Errorf("expected no installed bins, got %v", installed)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Errorf("expected stale shim %s to be removed, stat err = %v", stalePath, err)
	}
}

func TestSyncBinShims_RemovesOldAndCreatesNew(t *testing.T) {
	withHome(t)
	fakeMallet(t)
	binDir, err := store.BinDir()
	if err != nil {
		t.Fatal(err)
	}
	stalePath := filepath.Join(binDir, "old")
	writeFakeShim(t, stalePath)

	installed, errs := syncBinShims(&config.Settings{}, &journal{}, t.TempDir(), map[string]string{"old": "bin/old"}, map[string]string{"new": "bin/new"}, false)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if _, ok := installed["new"]; !ok {
		t.Errorf("expected new to be recorded as installed, got %v", installed)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Errorf("expected old shim to be removed")
	}
	shimDir, err := store.ShimDir()
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(filepath.Join(shimDir, "mallet-args.txt"))
	if err != nil {
		t.Fatalf("expected mallet to have been invoked: %v", err)
	}
	if !strings.Contains(string(args), "new") {
		t.Errorf("expected mallet args to reference the new shim name: %s", args)
	}
}

// TestSyncBinShims_ForceReplacesAForeignFile covers the one thing --force
// means: a file in the bin dir that ghpm did not stamp stops a shim, the error
// says how to get past it, and sync --force is a way past it. It used to be
// that only add honored the flag, so the hint was a dead end on sync.
func TestSyncBinShims_ForceReplacesAForeignFile(t *testing.T) {
	withHome(t)
	fakeMallet(t)
	binDir, err := store.BinDir()
	if err != nil {
		t.Fatal(err)
	}
	shimDir, err := store.ShimDir()
	if err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(binDir, "tool")
	if err := os.WriteFile(foreign, []byte("somebody else's binary"), 0755); err != nil {
		t.Fatal(err)
	}
	bins := map[string]string{"tool": "bin/tool"}

	installed, errs := syncBinShims(&config.Settings{}, &journal{}, t.TempDir(), nil, bins, false)
	if len(errs) != 1 || !strings.Contains(errs[0].err.Error(), "--force") || len(installed) != 0 {
		t.Fatalf("expected the foreign file to stop the shim and name --force, got %v, %v", installed, errs)
	}
	if _, err := os.Stat(filepath.Join(shimDir, "mallet-args.txt")); !os.IsNotExist(err) {
		t.Fatal("expected mallet not to be run over a foreign file")
	}

	installed, errs = syncBinShims(&config.Settings{}, &journal{}, t.TempDir(), nil, bins, true)
	if len(errs) != 0 || installed["tool"] != "bin/tool" {
		t.Fatalf("expected --force to stamp over the foreign file, got %v, %v", installed, errs)
	}
	if _, err := os.Stat(filepath.Join(shimDir, "mallet-args.txt")); err != nil {
		t.Fatalf("expected mallet to have been run: %v", err)
	}
}

// TestSyncBinShims_FailedCreateNotRecorded guards against a failed shim.Create
// still reading back as installed, the same class of bug as add.go's applyShimPlan.
func TestSyncBinShims_FailedCreateNotRecorded(t *testing.T) {
	withHome(t)
	binDir, err := store.BinDir()
	if err != nil {
		t.Fatal(err)
	}
	// No fakeMallet staged, so shim.Create fails for every entry in newBin.
	installed, errs := syncBinShims(&config.Settings{}, &journal{}, t.TempDir(), map[string]string{"gh": "bin/gh"}, map[string]string{"gh": "bin/gh"}, false)
	if len(errs) == 0 {
		t.Fatal("expected an error when mallet is not staged")
	}
	if _, ok := installed["gh"]; ok {
		t.Errorf("expected a failed create to not be recorded as installed, got %v", installed)
	}
	if _, statErr := os.Stat(filepath.Join(binDir, "gh")); !os.IsNotExist(statErr) {
		t.Errorf("expected no shim file left behind")
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

	installed, errs, err := syncPkgFonts(&config.Settings{}, &journal{}, t.TempDir(), "", map[string]string{"hack": "Hack-Regular.ttf"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected font errors: %v", errs)
	}
	if len(installed) != 0 {
		t.Errorf("expected no installed fonts, got %v", installed)
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

	_, errs, err := syncPkgFonts(&config.Settings{}, &journal{}, t.TempDir(), "", nil, nil)
	if err != nil || len(errs) != 0 {
		t.Fatalf("expected a clean no-op, got errs=%v err=%v", errs, err)
	}
	if _, statErr := os.Stat(wantDir); !os.IsNotExist(statErr) {
		t.Errorf("expected fonts dir to never be created when both maps are empty")
	}
}

// TestSyncPkgFonts_FailedInstallNotRecorded guards against a failed
// installFont call still reading back as installed.
func TestSyncPkgFonts_FailedInstallNotRecorded(t *testing.T) {
	home := withHome(t)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg-data"))

	// No source file exists at pkgDir/Missing.ttf, so installFont's copy fails.
	installed, errs, err := syncPkgFonts(&config.Settings{}, &journal{}, t.TempDir(), "", nil, map[string]string{"missing": "Missing.ttf"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) == 0 {
		t.Fatal("expected an error for a missing source font")
	}
	if _, ok := installed["missing"]; ok {
		t.Errorf("expected the failed font to not be recorded as installed, got %v", installed)
	}
}
