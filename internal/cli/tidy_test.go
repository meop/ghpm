package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/meop/ghpm/internal/config"
)

func withHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	return tmp
}

func makeBinDir(t *testing.T, home string, files ...string) string {
	t.Helper()
	binDir := filepath.Join(home, ".ghpm", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte{}, 0755); err != nil {
			t.Fatal(err)
		}
	}
	return binDir
}

func TestCleanBrokenInstalls_MissingShim(t *testing.T) {
	home := withHome(t)
	yes = true
	defer func() { yes = false }()

	pkgsDir := filepath.Join(home, ".ghpm", "extract")
	if err := os.MkdirAll(filepath.Join(pkgsDir, "fzf", "0.58.0"), 0755); err != nil {
		t.Fatal(err)
	}
	downloadDir := filepath.Join(home, ".ghpm", "download")

	manifest := &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0", BinNames: []string{"fzf"}}},
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
	home := withHome(t)
	yes = true
	defer func() { yes = false }()

	makeBinDir(t, home, "fzf")
	downloadDir := filepath.Join(home, ".ghpm", "download")

	manifest := &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0", BinNames: []string{"fzf"}}},
	}

	cleanBrokenInstalls(nil, manifest, downloadDir)

	if _, ok := manifest.Extracts["fzf"]; ok {
		t.Error("manifest entry was not removed")
	}
	binDir := filepath.Join(home, ".ghpm", "bin")
	if _, err := os.Lstat(filepath.Join(binDir, "fzf")); !os.IsNotExist(err) {
		t.Error("shim was not removed")
	}
}

func TestCleanBrokenInstalls_HealthyInstall(t *testing.T) {
	home := withHome(t)
	yes = true
	defer func() { yes = false }()

	makeBinDir(t, home, "fzf")
	pkgsDir := filepath.Join(home, ".ghpm", "extract")
	if err := os.MkdirAll(filepath.Join(pkgsDir, "fzf", "0.58.0"), 0755); err != nil {
		t.Fatal(err)
	}
	downloadDir := filepath.Join(home, ".ghpm", "download")

	manifest := &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0", BinNames: []string{"fzf"}}},
	}

	if cleaned := cleanBrokenInstalls(nil, manifest, downloadDir); cleaned {
		t.Error("should not have reported anything to clean")
	}
	if _, ok := manifest.Extracts["fzf"]; !ok {
		t.Error("manifest entry was incorrectly removed")
	}
}

func TestCleanBrokenInstalls_PartialShim(t *testing.T) {
	home := withHome(t)
	yes = true
	defer func() { yes = false }()

	makeBinDir(t, home, "uv")
	pkgsDir := filepath.Join(home, ".ghpm", "extract")
	if err := os.MkdirAll(filepath.Join(pkgsDir, "uv", "0.7.0"), 0755); err != nil {
		t.Fatal(err)
	}
	downloadDir := filepath.Join(home, ".ghpm", "download")

	manifest := &config.Manifest{
		Repos:    map[string]string{"uv": "github.com/astral-sh/uv"},
		Extracts: map[string]config.PackageEntry{"uv": {Version: "0.7.0", BinNames: []string{"uv", "uvx"}}},
	}

	if cleaned := cleanBrokenInstalls(nil, manifest, downloadDir); !cleaned {
		t.Error("should have reported partial shim to update manifest")
	}
	entry, ok := manifest.Extracts["uv"]
	if !ok {
		t.Error("manifest entry was removed but should be kept")
	}
	if len(entry.BinNames) != 1 || entry.BinNames[0] != "uv" {
		t.Errorf("expected BinNames=[uv], got %v", entry.BinNames)
	}
	binDir := filepath.Join(home, ".ghpm", "bin")
	if _, err := os.Lstat(filepath.Join(binDir, "uv")); err != nil {
		t.Error("uv shim was removed but should have been kept")
	}
	if _, err := os.Lstat(filepath.Join(pkgsDir, "uv", "0.7.0")); err != nil {
		t.Error("extract dir was removed but should have been kept")
	}
}

func TestCleanOrphanedBinShims_OrphanedShim(t *testing.T) {
	home := withHome(t)
	yes = true
	defer func() { yes = false }()

	binDir := makeBinDir(t, home, "fzf", "orphan")
	pkgsDir := filepath.Join(home, ".ghpm", "extract")
	if err := os.MkdirAll(filepath.Join(pkgsDir, "fzf", "0.58.0"), 0755); err != nil {
		t.Fatal(err)
	}

	manifest := &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0", BinNames: []string{"fzf"}}},
	}

	cleanOrphanedBinShims(manifest)

	if _, err := os.Lstat(filepath.Join(binDir, "fzf")); err != nil {
		t.Error("fzf shim was removed but should have been kept")
	}
	if _, err := os.Lstat(filepath.Join(binDir, "orphan")); !os.IsNotExist(err) {
		t.Error("orphan shim was not removed")
	}
}

func TestCleanOrphanedBinShims_KeepsSelfManaged(t *testing.T) {
	home := withHome(t)
	yes = true
	defer func() { yes = false }()

	binDir := makeBinDir(t, home, "gh", "ghpm", "orphan")

	manifest := &config.Manifest{
		Repos:    map[string]string{},
		Extracts: map[string]config.PackageEntry{},
	}

	cleanOrphanedBinShims(manifest)

	for _, name := range []string{"gh", "ghpm"} {
		if _, err := os.Lstat(filepath.Join(binDir, name)); err != nil {
			t.Errorf("%s was removed but should have been kept", name)
		}
	}
	if _, err := os.Lstat(filepath.Join(binDir, "orphan")); !os.IsNotExist(err) {
		t.Error("orphan was not removed")
	}
}

func TestCleanOrphanedExtracts_OrphanedExtract(t *testing.T) {
	home := withHome(t)
	yes = true
	defer func() { yes = false }()

	pkgsDir := filepath.Join(home, ".ghpm", "extract")
	if err := os.MkdirAll(filepath.Join(pkgsDir, "orphan", "1.0"), 0755); err != nil {
		t.Fatal(err)
	}

	cleanOrphanedExtracts(&config.Manifest{
		Repos:    map[string]string{},
		Extracts: map[string]config.PackageEntry{},
	})

	if _, err := os.Lstat(filepath.Join(pkgsDir, "orphan")); !os.IsNotExist(err) {
		t.Error("orphan extract dir was not removed")
	}
}

func TestCleanOrphanedExtracts_StaleVersion(t *testing.T) {
	home := withHome(t)
	yes = true
	defer func() { yes = false }()

	makeBinDir(t, home, "fzf")
	pkgsDir := filepath.Join(home, ".ghpm", "extract")
	if err := os.MkdirAll(filepath.Join(pkgsDir, "fzf", "0.57.0"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pkgsDir, "fzf", "0.58.0"), 0755); err != nil {
		t.Fatal(err)
	}

	manifest := &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0", BinNames: []string{"fzf"}}},
	}

	cleanOrphanedExtracts(manifest)

	if _, err := os.Lstat(filepath.Join(pkgsDir, "fzf", "0.57.0")); !os.IsNotExist(err) {
		t.Error("stale version 0.57.0 was not removed")
	}
	if _, err := os.Lstat(filepath.Join(pkgsDir, "fzf", "0.58.0")); err != nil {
		t.Errorf("current version 0.58.0 was removed: %v", err)
	}
}
