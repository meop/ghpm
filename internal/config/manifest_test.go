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
			"fzf": {Pin: "latest", Version: "0.56.0", Asset: map[string]AssetEntry{
				"fzf-0.56.0-linux_amd64.tar.gz": {Bin: map[string]string{"fzf": "fzf"}},
			}},
			"bun": {Pin: "latest", Version: "1.3.13", Asset: map[string]AssetEntry{
				"bun-linux-x64.zip": {Bin: map[string]string{"bun": "bun"}},
			}},
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
	if _, ok := entry.Asset["fzf-0.56.0-linux_amd64.tar.gz"]; !ok {
		t.Errorf("unexpected assets: %v", entry.Asset)
	}
	if loaded.Repos["fzf"] != "github.com/junegunn/fzf" {
		t.Errorf("unexpected source: %s", loaded.Repos["fzf"])
	}

	bunEntry := loaded.Extracts["bun"]
	if _, ok := bunEntry.Asset["bun-linux-x64.zip"]; !ok {
		t.Errorf("unexpected bun assets: %v", bunEntry.Asset)
	}
}

func TestAllFonts_MultiAsset(t *testing.T) {
	p := PackageEntry{
		Asset: map[string]AssetEntry{
			// asset key = release filename; font key = user-given name, value = file path
			"Hack.zip":     {Font: map[string]string{"hack": "Hack/Hack-Regular.ttf"}},
			"FiraCode.zip": {Font: map[string]string{"firacode": "FiraCode/FiraCode-Regular.ttf"}},
		},
	}
	fonts := p.AllFonts()
	if len(fonts) != 2 {
		t.Fatalf("expected 2, got %d: %v", len(fonts), fonts)
	}
	if fonts["hack"] != "Hack/Hack-Regular.ttf" {
		t.Errorf("hack missing: %v", fonts)
	}
	if fonts["firacode"] != "FiraCode/FiraCode-Regular.ttf" {
		t.Errorf("firacode missing: %v", fonts)
	}
}

func TestMixedAsset_BinAndFont(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	// A single asset that ships both an executable and a bundled font.
	m := &Manifest{
		Repos: map[string]string{"tool": "github.com/acme/tool"},
		Extracts: map[string]PackageEntry{
			"tool": {Pin: "latest", Version: "1.0.0", Asset: map[string]AssetEntry{
				"tool-linux.tar.gz": {
					Bin:  map[string]string{"tool": "tool"},
					Font: map[string]string{"tool-mono": "fonts/ToolMono.ttf"},
				},
			}},
		},
	}
	if err := saveManifestFile(m, path); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadManifestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	entry := loaded.Extracts["tool"]
	ae := entry.Asset["tool-linux.tar.gz"]
	if !ae.IsBin() || !ae.IsFont() {
		t.Errorf("expected asset to be both bin and font, got bin=%v font=%v", ae.IsBin(), ae.IsFont())
	}
	if entry.AllBins()["tool"] != "tool" {
		t.Errorf("bin lost: %v", entry.AllBins())
	}
	if entry.AllFonts()["tool-mono"] != "fonts/ToolMono.ttf" {
		t.Errorf("font lost: %v", entry.AllFonts())
	}
}

func TestAllFonts_EmptyForBinPackage(t *testing.T) {
	p := PackageEntry{
		Asset: map[string]AssetEntry{
			"tool.tar.gz": {Bin: map[string]string{"tool": "tool"}},
		},
	}
	if fonts := p.AllFonts(); len(fonts) != 0 {
		t.Errorf("expected empty, got %v", fonts)
	}
}

func TestLoadManifestFont(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	// asset key = release filename; font key = user-given name, value = file path
	raw := `{
  "repo": {"nerd-fonts": "github.com/ryanoasis/nerd-fonts"},
  "extract": {
    "nerd-fonts": {"pin": "latest", "version": "3.3.0", "asset": {
      "Hack.zip": {"font": {"hack": "Hack/Hack-Regular.ttf", "hack-bold": "Hack/Hack-Bold.ttf"}},
      "FiraCode.zip": {"font": {"firacode": "FiraCode/FiraCode-Regular.ttf"}}
    }}
  }
}`
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	m, err := loadManifestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := m.Extracts["nerd-fonts"]
	if !ok {
		t.Fatal("nerd-fonts entry missing")
	}
	if len(entry.AllBins()) != 0 {
		t.Errorf("expected no bins, got %v", entry.AllBins())
	}
	fonts := entry.AllFonts()
	if len(fonts) != 3 {
		t.Fatalf("expected 3 fonts, got %d: %v", len(fonts), fonts)
	}
	if fonts["hack"] != "Hack/Hack-Regular.ttf" {
		t.Errorf("hack missing or wrong path: %v", fonts)
	}
	if fonts["hack-bold"] != "Hack/Hack-Bold.ttf" {
		t.Errorf("hack-bold missing or wrong path: %v", fonts)
	}
	if fonts["firacode"] != "FiraCode/FiraCode-Regular.ttf" {
		t.Errorf("firacode missing or wrong path: %v", fonts)
	}
}

func TestAtomicSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	m := &Manifest{
		Repos:    map[string]string{},
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
