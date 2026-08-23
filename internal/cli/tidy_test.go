package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/store"
	"github.com/meop/ghpm/internal/ui"
)

func withHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("GHPM_TEST_HOME", tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("LOCALAPPDATA", filepath.Join(tmp, "AppData", "Local"))
	// userFontDir prefers XDG_DATA_HOME over HOME, so a home-only override leaves
	// font paths pointing at the real user's directories
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, ".local", "share"))
	return tmp
}

func makeBinDir(t *testing.T, files ...string) string {
	t.Helper()
	binDir, err := store.BinDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		// a fixture bin has to look like a shim ghpm stamped, or tidy will rightly
		// leave it alone as somebody else's binary
		writeFakeShim(t, filepath.Join(binDir, name))
	}
	return binDir
}

func TestCleanBrokenInstalls_MissingShim(t *testing.T) {
	withHome(t)
	yes = true
	defer func() { yes = false }()

	pkgsDir, err := store.ExtractsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pkgsDir, "fzf", "0.58.0"), 0755); err != nil {
		t.Fatal(err)
	}
	downloadDir, err := store.ReleaseBaseDir()
	if err != nil {
		t.Fatal(err)
	}

	manifest := &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0", Assets: []string{"fzf.tar.gz"}, Bin: map[string]string{"fzf": "fzf"}}},
	}

	cleanBrokenInstalls(nil, manifest, downloadDir)

	if _, ok := manifest.Extracts["fzf"]; ok {
		t.Error("manifest entry was not removed")
	}
	if _, err := os.Lstat(filepath.Join(pkgsDir, "fzf", "0.58.0")); !os.IsNotExist(err) {
		t.Error("extract dir was not removed")
	}
}

func TestCleanBrokenInstalls_MissingExtract(t *testing.T) {
	withHome(t)
	yes = true
	defer func() { yes = false }()

	binDir := makeBinDir(t, "fzf")
	downloadDir, err := store.ReleaseBaseDir()
	if err != nil {
		t.Fatal(err)
	}

	manifest := &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0", Assets: []string{"fzf.tar.gz"}, Bin: map[string]string{"fzf": "fzf"}}},
	}

	cleanBrokenInstalls(nil, manifest, downloadDir)

	if _, ok := manifest.Extracts["fzf"]; ok {
		t.Error("manifest entry was not removed")
	}
	if _, err := os.Lstat(filepath.Join(binDir, "fzf")); !os.IsNotExist(err) {
		t.Error("shim was not removed")
	}
}

func TestCleanBrokenInstalls_HealthyInstall(t *testing.T) {
	withHome(t)
	yes = true
	defer func() { yes = false }()

	makeBinDir(t, "fzf")
	pkgsDir, err := store.ExtractsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pkgsDir, "fzf", "0.58.0"), 0755); err != nil {
		t.Fatal(err)
	}
	downloadDir, err := store.ReleaseBaseDir()
	if err != nil {
		t.Fatal(err)
	}

	manifest := &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0", Assets: []string{"fzf.tar.gz"}, Bin: map[string]string{"fzf": "fzf"}}},
	}

	if cleaned := cleanBrokenInstalls(nil, manifest, downloadDir); cleaned {
		t.Error("should not have reported anything to clean")
	}
	if _, ok := manifest.Extracts["fzf"]; !ok {
		t.Error("manifest entry was incorrectly removed")
	}
}

func TestCleanBrokenInstalls_PartialShim_TrimsOne(t *testing.T) {
	withHome(t)
	yes = true
	defer func() { yes = false }()

	makeBinDir(t, "uv")
	pkgsDir, err := store.ExtractsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pkgsDir, "uv", "0.7.0"), 0755); err != nil {
		t.Fatal(err)
	}
	downloadDir, err := store.ReleaseBaseDir()
	if err != nil {
		t.Fatal(err)
	}

	manifest := &config.Manifest{
		Repos:    map[string]string{"uv": "github.com/astral-sh/uv"},
		Extracts: map[string]config.PackageEntry{"uv": {Version: "0.7.0", Assets: []string{"uv.tar.gz"}, Bin: map[string]string{"uv": "uv", "uvx": "uvx"}}},
	}

	if cleaned := cleanBrokenInstalls(nil, manifest, downloadDir); !cleaned {
		t.Error("should have reported missing shim")
	}
	entry, ok := manifest.Extracts["uv"]
	if !ok {
		t.Error("manifest entry was removed but should be kept")
	}
	if _, ok := entry.AllBins()["uv"]; !ok {
		t.Errorf("uv should remain in bins, got %v", entry.AllBins())
	}
	if _, ok := entry.AllBins()["uvx"]; ok {
		t.Errorf("uvx should have been trimmed from bins, got %v", entry.AllBins())
	}
	if _, err := os.Lstat(filepath.Join(pkgsDir, "uv", "0.7.0")); err != nil {
		t.Error("extract dir was removed but should have been kept")
	}
}

// TestCleanBrokenInstalls_DryRun_PrintsMessage locks in the fix for a dry run
// that showed a preview table and then went silent, with no way to tell that
// apart from a hang. Every tidy dry-run bail now prints msgDryRun.
func TestCleanBrokenInstalls_DryRun_PrintsMessage(t *testing.T) {
	withHome(t)
	dryRun = true
	defer func() { dryRun = false }()

	pkgsDir, err := store.ExtractsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pkgsDir, "fzf", "0.58.0"), 0755); err != nil {
		t.Fatal(err)
	}
	downloadDir, err := store.ReleaseBaseDir()
	if err != nil {
		t.Fatal(err)
	}

	manifest := &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0", Assets: []string{"fzf.tar.gz"}, Bin: map[string]string{"fzf": "fzf"}}},
	}

	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	if cleaned := cleanBrokenInstalls(nil, manifest, downloadDir); !cleaned {
		t.Fatal("expected the broken install to be reported")
	}

	out := buf.String()
	if !strings.Contains(out, msgDryRun) {
		t.Errorf("expected the dry-run closing message, got:\n%s", out)
	}
	if _, ok := manifest.Extracts["fzf"]; !ok {
		t.Error("dry-run should not actually remove the manifest entry")
	}
	if _, err := os.Lstat(filepath.Join(pkgsDir, "fzf", "0.58.0")); err != nil {
		t.Error("dry-run should not actually remove the extract dir")
	}
}

func TestCleanBrokenInstalls_AllShimsMissing_AutoRemovesExtract(t *testing.T) {
	withHome(t)
	yes = true
	defer func() { yes = false }()

	pkgsDir, err := store.ExtractsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pkgsDir, "fzf", "0.58.0"), 0755); err != nil {
		t.Fatal(err)
	}
	downloadDir, err := store.ReleaseBaseDir()
	if err != nil {
		t.Fatal(err)
	}

	manifest := &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0", Assets: []string{"fzf.tar.gz"}, Bin: map[string]string{"fzf": "fzf"}}},
	}

	cleanBrokenInstalls(nil, manifest, downloadDir)

	if _, ok := manifest.Extracts["fzf"]; ok {
		t.Error("manifest entry should have been auto-removed when last shim was trimmed")
	}
	if _, err := os.Lstat(filepath.Join(pkgsDir, "fzf", "0.58.0")); !os.IsNotExist(err) {
		t.Error("extract dir should have been removed when last shim was trimmed")
	}
}

func TestCleanOrphanedBinShims_OrphanedShim(t *testing.T) {
	withHome(t)
	yes = true
	defer func() { yes = false }()

	binDir := makeBinDir(t, "fzf", "orphan")
	pkgsDir, err := store.ExtractsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pkgsDir, "fzf", "0.58.0"), 0755); err != nil {
		t.Fatal(err)
	}

	manifest := &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0", Assets: []string{"fzf.tar.gz"}, Bin: map[string]string{"fzf": "fzf"}}},
	}

	cleanOrphanedBinShims(nil, manifest)

	if _, err := os.Lstat(filepath.Join(binDir, "fzf")); err != nil {
		t.Error("fzf shim was removed but should have been kept")
	}
	if _, err := os.Lstat(filepath.Join(binDir, "orphan")); !os.IsNotExist(err) {
		t.Error("orphan shim was not removed")
	}
}

func TestCleanOrphanedBinShims_KeepsSelfManaged(t *testing.T) {
	withHome(t)
	yes = true
	defer func() { yes = false }()

	binDir := makeBinDir(t, "ghpm", "orphan")

	manifest := &config.Manifest{
		Repos:    map[string]string{},
		Extracts: map[string]config.PackageEntry{},
	}

	cleanOrphanedBinShims(nil, manifest)

	// gh is a package now, not machinery, so bin/ exempts only ghpm itself
	for _, name := range []string{"ghpm"} {
		if _, err := os.Lstat(filepath.Join(binDir, name)); err != nil {
			t.Errorf("%s was removed but should have been kept", name)
		}
	}
	if _, err := os.Lstat(filepath.Join(binDir, "orphan")); !os.IsNotExist(err) {
		t.Error("orphan was not removed")
	}
}

// TestCleanOrphanedBinShims_ReportsActualRemovedCount is the regression test
// for a real gap: the closing "removed N orphaned bin(s)" line always reported
// the attempted count, even when some individual os.Remove calls failed and
// were silently discarded — the tally was optimistic, not actual. Here
// "orphan-stuck" is a non-empty directory, so os.Remove on it fails with
// ENOTEMPTY; this simulates a removal failure without relying on filesystem
// permissions, which behave inconsistently when tests run as root.
func TestCleanOrphanedBinShims_ReportsActualRemovedCount(t *testing.T) {
	withHome(t)
	yes = true
	defer func() { yes = false }()

	binDir := makeBinDir(t, "orphan-removable")
	stuckDir := filepath.Join(binDir, "orphan-stuck")
	if err := os.MkdirAll(stuckDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stuckDir, "child"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	manifest := &config.Manifest{
		Repos:    map[string]string{},
		Extracts: map[string]config.PackageEntry{},
	}

	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	cleanOrphanedBinShims(nil, manifest)

	if _, err := os.Lstat(filepath.Join(binDir, "orphan-removable")); !os.IsNotExist(err) {
		t.Error("orphan-removable should have been removed")
	}
	if _, err := os.Lstat(stuckDir); err != nil {
		t.Error("orphan-stuck should still exist — its removal was expected to fail")
	}

	out := buf.String()
	if !strings.Contains(out, "removed 1 orphaned bin(s)") {
		t.Errorf("expected the count to reflect the 1 actual success, not the 2 attempted, got:\n%s", out)
	}
	if strings.Contains(out, "removed 2 orphaned bin(s)") {
		t.Errorf("count should not include the failed removal, got:\n%s", out)
	}
}

func TestCleanOrphanedFonts_AllMissing(t *testing.T) {
	withHome(t)
	t.Setenv("XDG_DATA_HOME", "")
	yes = true
	defer func() { yes = false }()

	pkgsDir, err := store.ExtractsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pkgsDir, "nerd-fonts", "3.0.0"), 0755); err != nil {
		t.Fatal(err)
	}
	downloadDir, err := store.ReleaseBaseDir()
	if err != nil {
		t.Fatal(err)
	}

	manifest := &config.Manifest{
		Repos: map[string]string{"nerd-fonts": "github.com/ryanoasis/nerd-fonts"},
		Extracts: map[string]config.PackageEntry{"nerd-fonts": {Version: "3.0.0", Assets: []string{"Hack.tar.gz"}, Font: map[string]string{
			"hack-regular": "Hack/Hack-Regular.ttf",
			"hack-bold":    "Hack/Hack-Bold.ttf",
		}}},
	}

	cleaned := cleanOrphanedFonts(nil, manifest, downloadDir)
	if !cleaned {
		t.Fatal("expected orphaned fonts to be found")
	}

	entry, ok := manifest.Extracts["nerd-fonts"]
	if ok && (len(entry.AllFonts()) != 0 || len(entry.AllBins()) != 0) {
		t.Errorf("expected manifest entry to be fully removed, got %+v", entry)
	}
	if _, err := os.Lstat(filepath.Join(pkgsDir, "nerd-fonts", "3.0.0")); !os.IsNotExist(err) {
		t.Error("extract dir was not removed after all fonts cleaned")
	}
}

func TestCleanOrphanedFonts_OneMissingKeepsOther(t *testing.T) {
	withHome(t)
	t.Setenv("XDG_DATA_HOME", "")
	yes = true
	defer func() { yes = false }()

	pkgsDir, err := store.ExtractsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pkgsDir, "nerd-fonts", "3.0.0"), 0755); err != nil {
		t.Fatal(err)
	}
	downloadDir, err := store.ReleaseBaseDir()
	if err != nil {
		t.Fatal(err)
	}

	fontsDir, err := userFontDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fontsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fontsDir, "Hack-Regular.ttf"), []byte("font"), 0644); err != nil {
		t.Fatal(err)
	}

	manifest := &config.Manifest{
		Repos: map[string]string{"nerd-fonts": "github.com/ryanoasis/nerd-fonts"},
		Extracts: map[string]config.PackageEntry{"nerd-fonts": {Version: "3.0.0", Assets: []string{"Hack.tar.gz"}, Font: map[string]string{
			"hack-regular": "Hack/Hack-Regular.ttf",
			"hack-bold":    "Hack/Hack-Bold.ttf",
		}}},
	}

	cleaned := cleanOrphanedFonts(nil, manifest, downloadDir)
	if !cleaned {
		t.Fatal("expected orphaned font to be found")
	}

	entry, ok := manifest.Extracts["nerd-fonts"]
	if !ok {
		t.Fatal("manifest entry was fully removed, but hack-regular is still installed")
	}
	if _, hasBold := entry.AllFonts()["hack-bold"]; hasBold {
		t.Error("hack-bold should have been removed from manifest")
	}
	if _, hasRegular := entry.AllFonts()["hack-regular"]; !hasRegular {
		t.Error("hack-regular should remain in manifest")
	}
	if _, err := os.Lstat(filepath.Join(pkgsDir, "nerd-fonts", "3.0.0")); err != nil {
		t.Error("extract dir was removed but should be kept since hack-regular is still installed")
	}
}

func TestCleanOrphanedExtracts_OrphanedExtract(t *testing.T) {
	withHome(t)
	yes = true
	defer func() { yes = false }()

	pkgsDir, err := store.ExtractsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pkgsDir, "orphan", "1.0"), 0755); err != nil {
		t.Fatal(err)
	}

	cleanOrphanedExtracts(nil, &config.Manifest{
		Repos:    map[string]string{},
		Extracts: map[string]config.PackageEntry{},
	})

	if _, err := os.Lstat(filepath.Join(pkgsDir, "orphan")); !os.IsNotExist(err) {
		t.Error("orphan extract dir was not removed")
	}
}

func TestCleanOrphanedExtracts_StaleVersion(t *testing.T) {
	withHome(t)
	yes = true
	defer func() { yes = false }()

	makeBinDir(t, "fzf")
	pkgsDir, err := store.ExtractsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pkgsDir, "fzf", "0.57.0"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pkgsDir, "fzf", "0.58.0"), 0755); err != nil {
		t.Fatal(err)
	}

	manifest := &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0", Assets: []string{"fzf.tar.gz"}, Bin: map[string]string{"fzf": "fzf"}}},
	}

	cleanOrphanedExtracts(nil, manifest)

	if _, err := os.Lstat(filepath.Join(pkgsDir, "fzf", "0.57.0")); !os.IsNotExist(err) {
		t.Error("stale version 0.57.0 was not removed")
	}
	if _, err := os.Lstat(filepath.Join(pkgsDir, "fzf", "0.58.0")); err != nil {
		t.Errorf("current version 0.58.0 was removed: %v", err)
	}
}
