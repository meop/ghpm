package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/meop/ghpm/internal/config"
)

func makeFontFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("fake font"), 0644); err != nil {
		t.Fatal(err)
	}
}

func fontPkg(assetName string, fonts map[string]string) config.PackageEntry {
	return config.PackageEntry{
		Version: "3.3.0",
		Asset:   map[string]config.AssetEntry{assetName: {Font: fonts}},
	}
}

func TestFontInstalled_Present(t *testing.T) {
	tmp := t.TempDir()
	makeFontFile(t, tmp, "Hack-Regular.ttf")
	if !fontInstalled("Hack-Regular.ttf", tmp) {
		t.Error("expected true when file exists")
	}
}

func TestFontInstalled_Missing(t *testing.T) {
	tmp := t.TempDir()
	if fontInstalled("Hack-Regular.ttf", tmp) {
		t.Error("expected false when file missing")
	}
}

func TestFontInstalled_SubdirKey(t *testing.T) {
	tmp := t.TempDir()
	makeFontFile(t, tmp, "Hack-Regular.ttf")
	// fontKey may include a leading subdir; only the base name is checked in fontsDir
	if !fontInstalled("fonts/Hack-Regular.ttf", tmp) {
		t.Error("expected true — only base name should be matched in fontsDir")
	}
}

func TestStaleFontPaths_KeepsReinstalled(t *testing.T) {
	old := map[string]string{
		"hack":  "Hack/Hack-Regular.ttf",
		"hackb": "Hack/Hack-Bold.ttf",
	}
	// New version ships the same Regular file but drops Bold.
	newPaths := []string{"Hack/Hack-Regular.ttf"}

	stale := staleFontPaths(old, newPaths)

	if len(stale) != 1 || stale[0] != "Hack/Hack-Bold.ttf" {
		t.Errorf("expected only Hack-Bold.ttf to be stale, got %v", stale)
	}
}

func TestStaleFontPaths_MatchesByBaseName(t *testing.T) {
	// Old and new paths differ by directory but share the base name; the file
	// must be kept because the install step just wrote it.
	old := map[string]string{"hack": "v2/Hack-Regular.ttf"}
	newPaths := []string{"v3/Hack-Regular.ttf"}

	if stale := staleFontPaths(old, newPaths); len(stale) != 0 {
		t.Errorf("expected nothing stale when base names match, got %v", stale)
	}
}

func TestStaleFontPaths_AllRemovedWhenNoneReinstalled(t *testing.T) {
	old := map[string]string{"a": "A.ttf", "b": "B.otf"}

	stale := staleFontPaths(old, nil)

	if len(stale) != 2 {
		t.Errorf("expected both fonts stale, got %v", stale)
	}
}

func TestCleanBrokenInstalls_FontMissing(t *testing.T) {
	home := withHome(t)
	xdgDir := filepath.Join(home, ".xdg")
	t.Setenv("XDG_DATA_HOME", xdgDir)
	yes = true
	defer func() { yes = false }()

	pkgsDir := filepath.Join(home, ".ghpm", "extract")
	if err := os.MkdirAll(filepath.Join(pkgsDir, "nerd-fonts", "3.3.0"), 0755); err != nil {
		t.Fatal(err)
	}
	downloadDir := filepath.Join(home, ".ghpm", "download")

	manifest := &config.Manifest{
		Repos:    map[string]string{"nerd-fonts": "github.com/ryanoasis/nerd-fonts"},
		Extracts: map[string]config.PackageEntry{"nerd-fonts": fontPkg("Hack.zip", map[string]string{"hack": "Hack/Hack-Regular.ttf"})},
	}

	if cleaned := cleanBrokenInstalls(nil, manifest, downloadDir); !cleaned {
		t.Error("expected broken install to be detected")
	}
	if _, ok := manifest.Extracts["nerd-fonts"]; ok {
		t.Error("manifest entry should have been removed")
	}
	if _, err := os.Lstat(filepath.Join(pkgsDir, "nerd-fonts", "3.3.0")); !os.IsNotExist(err) {
		t.Error("extract dir should have been removed")
	}
}

func TestCleanBrokenInstalls_FontHealthy(t *testing.T) {
	home := withHome(t)
	xdgDir := filepath.Join(home, ".xdg")
	t.Setenv("XDG_DATA_HOME", xdgDir)
	fontsDir := filepath.Join(xdgDir, "fonts")
	makeFontFile(t, fontsDir, "Hack-Regular.ttf")
	yes = true
	defer func() { yes = false }()

	pkgsDir := filepath.Join(home, ".ghpm", "extract")
	if err := os.MkdirAll(filepath.Join(pkgsDir, "nerd-fonts", "3.3.0"), 0755); err != nil {
		t.Fatal(err)
	}
	downloadDir := filepath.Join(home, ".ghpm", "download")

	manifest := &config.Manifest{
		Repos:    map[string]string{"nerd-fonts": "github.com/ryanoasis/nerd-fonts"},
		Extracts: map[string]config.PackageEntry{"nerd-fonts": fontPkg("Hack.zip", map[string]string{"hack": "Hack/Hack-Regular.ttf"})},
	}

	if cleaned := cleanBrokenInstalls(nil, manifest, downloadDir); cleaned {
		t.Error("healthy font install should not be flagged")
	}
	if _, ok := manifest.Extracts["nerd-fonts"]; !ok {
		t.Error("manifest entry should not have been removed")
	}
}
