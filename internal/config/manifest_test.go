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
	if m.Tools == nil {
		t.Error("expected non-nil tools map")
	}
	if m.Installs == nil {
		t.Error("expected non-nil installs map")
	}
	if len(m.Installs) != 0 {
		t.Error("expected empty installs map for missing file")
	}
}

func TestSaveAndLoadManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	m := &Manifest{
		Tools: map[string]string{
			"fzf": "github.com/junegunn/fzf",
		},
		Installs: map[string]PackageEntry{
			"fzf": {Pin: "latest", Version: "0.56.0"},
		},
	}

	if err := saveManifestFile(m, path); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadManifestFile(path)
	if err != nil {
		t.Fatal(err)
	}

	entry, ok := loaded.Installs["fzf"]
	if !ok {
		t.Fatal("fzf entry missing after reload")
	}
	if entry.Version != "0.56.0" {
		t.Errorf("unexpected version: %s", entry.Version)
	}
	if entry.Pin != "latest" {
		t.Errorf("unexpected pin: %s", entry.Pin)
	}
	if loaded.Tools["fzf"] != "github.com/junegunn/fzf" {
		t.Errorf("unexpected source: %s", loaded.Tools["fzf"])
	}
}

func TestAtomicSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	m := &Manifest{
		Tools:  map[string]string{},
		Installs: map[string]PackageEntry{},
	}
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
