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

func TestCleanOrphanedShims_RemovesOrphans(t *testing.T) {
	home := withHome(t)
	yes = true
	defer func() { yes = false }()

	binDir := makeBinDir(t, home, "fzf", "orphan")

	manifest := &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {BinName: "fzf"}},
	}

	cleanOrphanedShims(nil, manifest)

	if _, err := os.Lstat(filepath.Join(binDir, "fzf")); err != nil {
		t.Error("fzf shim was removed but should have been kept")
	}
	if _, err := os.Lstat(filepath.Join(binDir, "orphan")); !os.IsNotExist(err) {
		t.Error("orphan was not removed")
	}
}

func TestCleanOrphanedPackages_RemovesOrphans(t *testing.T) {
	home := withHome(t)
	yes = true
	defer func() { yes = false }()

	pkgsDir := filepath.Join(home, ".ghpm", "extract")
	if err := os.MkdirAll(filepath.Join(pkgsDir, "orphan", "1.0"), 0755); err != nil {
		t.Fatal(err)
	}

	cleanOrphanedPackages(nil, &config.Manifest{
		Repos:    map[string]string{},
		Extracts: map[string]config.PackageEntry{},
	})

	if _, err := os.Lstat(filepath.Join(pkgsDir, "orphan")); !os.IsNotExist(err) {
		t.Error("orphan dir was not removed")
	}
}

func TestCleanOrphanedPackages_KeepsCurrentVersion(t *testing.T) {
	home := withHome(t)
	yes = true
	defer func() { yes = false }()

	pkgsDir := filepath.Join(home, ".ghpm", "extract")
	if err := os.MkdirAll(filepath.Join(pkgsDir, "fzf", "0.58.0"), 0755); err != nil {
		t.Fatal(err)
	}

	cleanOrphanedPackages(nil, &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0"}},
	})

	if _, err := os.Lstat(filepath.Join(pkgsDir, "fzf", "0.58.0")); err != nil {
		t.Errorf("current version was removed: %v", err)
	}
}

func TestCleanOrphanedPackages_RemovesStaleVersion(t *testing.T) {
	home := withHome(t)
	yes = true
	defer func() { yes = false }()

	pkgsDir := filepath.Join(home, ".ghpm", "extract")
	if err := os.MkdirAll(filepath.Join(pkgsDir, "fzf", "0.57.0"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pkgsDir, "fzf", "0.58.0"), 0755); err != nil {
		t.Fatal(err)
	}

	cleanOrphanedPackages(nil, &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0"}},
	})

	if _, err := os.Lstat(filepath.Join(pkgsDir, "fzf", "0.57.0")); !os.IsNotExist(err) {
		t.Error("stale version 0.57.0 was not removed")
	}
	if _, err := os.Lstat(filepath.Join(pkgsDir, "fzf", "0.58.0")); err != nil {
		t.Errorf("current version 0.58.0 was removed: %v", err)
	}
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
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0", BinName: "fzf"}},
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
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0", BinName: "fzf"}},
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
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0", BinName: "fzf"}},
	}

	if cleaned := cleanBrokenInstalls(nil, manifest, downloadDir); cleaned {
		t.Error("should not have reported anything to clean")
	}
	if _, ok := manifest.Extracts["fzf"]; !ok {
		t.Error("manifest entry was incorrectly removed")
	}
}

func TestCleanOrphanedShims_KeepsSelfManaged(t *testing.T) {
	home := withHome(t)
	yes = true
	defer func() { yes = false }()

	binDir := makeBinDir(t, home, "gh", "ghpm", "orphan")

	cleanOrphanedShims(nil, &config.Manifest{
		Repos:    map[string]string{},
		Extracts: map[string]config.PackageEntry{},
	})

	for _, name := range []string{"gh", "ghpm"} {
		if _, err := os.Lstat(filepath.Join(binDir, name)); err != nil {
			t.Errorf("%s was removed but should have been kept", name)
		}
	}
	if _, err := os.Lstat(filepath.Join(binDir, "orphan")); !os.IsNotExist(err) {
		t.Error("orphan was not removed")
	}
}
