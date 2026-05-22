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

func TestCleanBrokenLinkage_MissingShim(t *testing.T) {
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

	cleanBrokenLinkage(nil, manifest, downloadDir)

	if _, ok := manifest.Extracts["fzf"]; ok {
		t.Error("manifest entry was not removed")
	}
	if _, err := os.Lstat(filepath.Join(pkgsDir, "fzf", "0.58.0")); !os.IsNotExist(err) {
		t.Error("extract dir was not removed")
	}
}

func TestCleanBrokenLinkage_MissingExtract(t *testing.T) {
	home := withHome(t)
	yes = true
	defer func() { yes = false }()

	makeBinDir(t, home, "fzf")
	downloadDir := filepath.Join(home, ".ghpm", "download")

	manifest := &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0", BinName: "fzf"}},
	}

	cleanBrokenLinkage(nil, manifest, downloadDir)

	if _, ok := manifest.Extracts["fzf"]; ok {
		t.Error("manifest entry was not removed")
	}
	binDir := filepath.Join(home, ".ghpm", "bin")
	if _, err := os.Lstat(filepath.Join(binDir, "fzf")); !os.IsNotExist(err) {
		t.Error("shim was not removed")
	}
}

func TestCleanBrokenLinkage_HealthyInstall(t *testing.T) {
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

	if cleaned := cleanBrokenLinkage(nil, manifest, downloadDir); cleaned {
		t.Error("should not have reported anything to clean")
	}
	if _, ok := manifest.Extracts["fzf"]; !ok {
		t.Error("manifest entry was incorrectly removed")
	}
}

func TestCleanBrokenLinkage_OrphanedShim(t *testing.T) {
	home := withHome(t)
	yes = true
	defer func() { yes = false }()

	binDir := makeBinDir(t, home, "fzf", "orphan")
	downloadDir := filepath.Join(home, ".ghpm", "download")
	pkgsDir := filepath.Join(home, ".ghpm", "extract")
	if err := os.MkdirAll(filepath.Join(pkgsDir, "fzf", "0.58.0"), 0755); err != nil {
		t.Fatal(err)
	}

	manifest := &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0", BinName: "fzf"}},
	}

	cleanBrokenLinkage(nil, manifest, downloadDir)

	if _, err := os.Lstat(filepath.Join(binDir, "fzf")); err != nil {
		t.Error("fzf shim was removed but should have been kept")
	}
	if _, err := os.Lstat(filepath.Join(binDir, "orphan")); !os.IsNotExist(err) {
		t.Error("orphan shim was not removed")
	}
}

func TestCleanBrokenLinkage_KeepsSelfManaged(t *testing.T) {
	home := withHome(t)
	yes = true
	defer func() { yes = false }()

	binDir := makeBinDir(t, home, "gh", "ghpm", "orphan")
	downloadDir := filepath.Join(home, ".ghpm", "download")

	manifest := &config.Manifest{
		Repos:    map[string]string{},
		Extracts: map[string]config.PackageEntry{},
	}

	cleanBrokenLinkage(nil, manifest, downloadDir)

	for _, name := range []string{"gh", "ghpm"} {
		if _, err := os.Lstat(filepath.Join(binDir, name)); err != nil {
			t.Errorf("%s was removed but should have been kept", name)
		}
	}
	if _, err := os.Lstat(filepath.Join(binDir, "orphan")); !os.IsNotExist(err) {
		t.Error("orphan was not removed")
	}
}

func TestCleanBrokenLinkage_OrphanedExtract(t *testing.T) {
	home := withHome(t)
	yes = true
	defer func() { yes = false }()

	pkgsDir := filepath.Join(home, ".ghpm", "extract")
	if err := os.MkdirAll(filepath.Join(pkgsDir, "orphan", "1.0"), 0755); err != nil {
		t.Fatal(err)
	}
	downloadDir := filepath.Join(home, ".ghpm", "download")

	cleanBrokenLinkage(nil, &config.Manifest{
		Repos:    map[string]string{},
		Extracts: map[string]config.PackageEntry{},
	}, downloadDir)

	if _, err := os.Lstat(filepath.Join(pkgsDir, "orphan")); !os.IsNotExist(err) {
		t.Error("orphan extract dir was not removed")
	}
}

func TestCleanBrokenLinkage_StaleVersion(t *testing.T) {
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
	downloadDir := filepath.Join(home, ".ghpm", "download")

	manifest := &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0", BinName: "fzf"}},
	}

	cleanBrokenLinkage(nil, manifest, downloadDir)

	if _, err := os.Lstat(filepath.Join(pkgsDir, "fzf", "0.57.0")); !os.IsNotExist(err) {
		t.Error("stale version 0.57.0 was not removed")
	}
	if _, err := os.Lstat(filepath.Join(pkgsDir, "fzf", "0.58.0")); err != nil {
		t.Errorf("current version 0.58.0 was removed: %v", err)
	}
}
