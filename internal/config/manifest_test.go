package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadManifestMissing(t *testing.T) {
	m, err := loadManifestFile(filepath.Join(t.TempDir(), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Packages == nil {
		t.Error("expected non-nil packages map")
	}
	if len(m.Packages) != 0 {
		t.Error("expected empty packages map for missing file")
	}
}

func TestSaveAndLoadManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	m := &Manifest{
		Packages: map[string]PackageEntry{
			"fzf": {
				Source:       "github.com/junegunn/fzf",
				Version:      "v0.56.0",
				Versioned:    false,
				AssetPattern: "fzf-0.56.0-linux_amd64.tar.gz",
				InstalledAt:  "2026-04-23T10:00:00Z",
			},
		},
	}

	if err := saveManifestFile(m, path); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadManifestFile(path)
	if err != nil {
		t.Fatal(err)
	}

	entry, ok := loaded.Packages["fzf"]
	if !ok {
		t.Fatal("fzf entry missing after reload")
	}
	if entry.Version != "v0.56.0" {
		t.Errorf("unexpected version: %s", entry.Version)
	}
	if entry.Source != "github.com/junegunn/fzf" {
		t.Errorf("unexpected source: %s", entry.Source)
	}
}

func TestAtomicSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	m := &Manifest{Packages: map[string]PackageEntry{}}
	if err := saveManifestFile(m, path); err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "manifest.json" {
			t.Errorf("unexpected file after atomic save: %s", e.Name())
		}
	}
}
