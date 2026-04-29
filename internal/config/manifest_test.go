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
	if m.Repos == nil {
		t.Error("expected non-nil repos map")
	}
	if m.Extracts == nil {
		t.Error("expected non-nil installs map")
	}
	if len(m.Extracts) != 0 {
		t.Error("expected empty installs map for missing file")
	}
}

func TestSaveAndLoadManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	m := &Manifest{
		Repos: map[string]string{
			"fzf": "github.com/junegunn/fzf",
			"bun": "github.com/oven-sh/bun",
		},
		Extracts: map[string]PackageEntry{
			"fzf": {Pin: "latest", Version: "0.56.0", AssetName: "fzf-0.56.0-linux_amd64.tar.gz"},
			"bun": {Pin: "latest", Version: "1.3.13", AssetName: "bun-linux-x64.zip"},
		},
	}

	if err := saveManifestFile(m, path); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadManifestFile(path)
	if err != nil {
		t.Fatal(err)
	}

	entry, ok := loaded.Extracts["fzf"]
	if !ok {
		t.Fatal("fzf entry missing after reload")
	}
	if entry.Version != "0.56.0" {
		t.Errorf("unexpected version: %s", entry.Version)
	}
	if entry.Pin != "latest" {
		t.Errorf("unexpected pin: %s", entry.Pin)
	}
	if entry.AssetName != "fzf-0.56.0-linux_amd64.tar.gz" {
		t.Errorf("unexpected asset: %s", entry.AssetName)
	}
	if loaded.Repos["fzf"] != "github.com/junegunn/fzf" {
		t.Errorf("unexpected source: %s", loaded.Repos["fzf"])
	}

	bunEntry := loaded.Extracts["bun"]
	if bunEntry.AssetName != "bun-linux-x64.zip" {
		t.Errorf("unexpected bun asset: %s", bunEntry.AssetName)
	}
}

func TestAtomicSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	m := &Manifest{
		Repos:  map[string]string{},
		Extracts: map[string]PackageEntry{},
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
