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
			"fzf": {Pin: "latest", Version: "0.56.0",
				Assets: []string{"fzf-0.56.0-linux_amd64.tar.gz"},
				Bin:    map[string]string{"fzf": "fzf"}},
			"bun": {Pin: "latest", Version: "1.3.13",
				Assets: []string{"bun-linux-x64.zip"},
				Bin:    map[string]string{"bun": "bun"}},
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
	if len(entry.Assets) != 1 || entry.Assets[0] != "fzf-0.56.0-linux_amd64.tar.gz" {
		t.Errorf("unexpected assets: %v", entry.Assets)
	}
	if entry.Bin["fzf"] != "fzf" {
		t.Errorf("unexpected bin: %v", entry.Bin)
	}
	if loaded.Repos["fzf"] != "github.com/junegunn/fzf" {
		t.Errorf("unexpected source: %s", loaded.Repos["fzf"])
	}

	bunEntry := loaded.Extracts["bun"]
	if bunEntry.Bin["bun"] != "bun" {
		t.Errorf("unexpected bun bin: %v", bunEntry.Bin)
	}
}

func TestAllFonts_Overlay(t *testing.T) {
	// Fonts discovered across the overlaid extract are tracked as one flat map.
	p := PackageEntry{
		Assets: []string{"Hack.zip", "FiraCode.zip"},
		Font: map[string]string{
			"hack":     "Hack/Hack-Regular.ttf",
			"firacode": "FiraCode/FiraCode-Regular.ttf",
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

func TestMixed_BinAndFont(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	// A package whose overlay ships both an executable and a bundled font.
	m := &Manifest{
		Repos: map[string]string{"tool": "github.com/acme/tool"},
		Extracts: map[string]PackageEntry{
			"tool": {Pin: "latest", Version: "1.0.0",
				Assets: []string{"tool-linux.tar.gz"},
				Bin:    map[string]string{"tool": "tool"},
				Font:   map[string]string{"tool-mono": "fonts/ToolMono.ttf"}},
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
	if entry.AllBins()["tool"] != "tool" {
		t.Errorf("bin lost: %v", entry.AllBins())
	}
	if entry.AllFonts()["tool-mono"] != "fonts/ToolMono.ttf" {
		t.Errorf("font lost: %v", entry.AllFonts())
	}
}

func TestAllFonts_EmptyForBinPackage(t *testing.T) {
	p := PackageEntry{
		Assets: []string{"tool.tar.gz"},
		Bin:    map[string]string{"tool": "tool"},
	}
	if fonts := p.AllFonts(); len(fonts) != 0 {
		t.Errorf("expected empty, got %v", fonts)
	}
}

func TestLoadManifestFont(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	// font key = user-given name, value = file path within the overlaid extract
	raw := `{
  "repo": {"nerd-fonts": "github.com/ryanoasis/nerd-fonts"},
  "extract": {
    "nerd-fonts": {"pin": "latest", "version": "3.3.0",
      "assets": ["Hack.zip", "FiraCode.zip"],
      "font": {"hack": "Hack/Hack-Regular.ttf", "hack-bold": "Hack/Hack-Bold.ttf", "firacode": "FiraCode/FiraCode-Regular.ttf"}}
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

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

// TestDiscoveredBins is the crux of the subset-stability fix: the full set
// discovered at install time is the selected bins (Bin values) plus the declined
// ones, so sync can tell "user wanted a subset" apart from "the release grew new
// binaries". The codex case — one shimmed bin, two declined helpers.
func TestDiscoveredBins(t *testing.T) {
	p := PackageEntry{
		Bin:         map[string]string{"codex": "codex-aarch64-pc-windows-msvc.exe"},
		BinDeclined: []string{"codex-command-runner.exe", "codex-windows-sandbox-setup.exe"},
	}
	want := []string{
		"codex-aarch64-pc-windows-msvc.exe",
		"codex-command-runner.exe",
		"codex-windows-sandbox-setup.exe",
	}
	if got := p.DiscoveredBins(); !sameSet(got, want) {
		t.Errorf("DiscoveredBins() = %v, want set %v", got, want)
	}
}

// TestDiscoveredBins_Legacy: an entry written before declines were tracked has no
// BinDeclined, so only the selected keys are known. Such a package gets reprompted
// once on the next sync, then records its full set.
func TestDiscoveredBins_Legacy(t *testing.T) {
	p := PackageEntry{Bin: map[string]string{"codex": "codex.exe"}}
	if got := p.DiscoveredBins(); !sameSet(got, []string{"codex.exe"}) {
		t.Errorf("DiscoveredBins() = %v, want [codex.exe]", got)
	}
}

func TestDiscoveredFonts(t *testing.T) {
	p := PackageEntry{
		Font:         map[string]string{"hack": "Hack/Hack-Regular.ttf"},
		FontDeclined: []string{"Hack/Hack-Bold.ttf"},
	}
	want := []string{"Hack/Hack-Regular.ttf", "Hack/Hack-Bold.ttf"}
	if got := p.DiscoveredFonts(); !sameSet(got, want) {
		t.Errorf("DiscoveredFonts() = %v, want set %v", got, want)
	}
}

// TestDeclinedRoundTrip: the declined sets must survive a save/load so the next
// sync can reconstruct the full discovered set.
func TestDeclinedRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	m := &Manifest{
		Repos: map[string]string{"codex": "github.com/openai/codex"},
		Extracts: map[string]PackageEntry{
			"codex": {Pin: "latest", Version: "0.5.0",
				Assets:       []string{"codex-windows.zip"},
				Bin:          map[string]string{"codex": "codex-aarch64-pc-windows-msvc.exe"},
				BinDeclined:  []string{"codex-command-runner.exe", "codex-windows-sandbox-setup.exe"},
				Font:         map[string]string{"hack": "Hack-Regular.ttf"},
				FontDeclined: []string{"Hack-Bold.ttf"}},
		},
	}
	if err := saveManifestFile(m, path); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadManifestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	entry := loaded.Extracts["codex"]
	if !sameSet(entry.BinDeclined, []string{"codex-command-runner.exe", "codex-windows-sandbox-setup.exe"}) {
		t.Errorf("bin_declined lost: %v", entry.BinDeclined)
	}
	if !sameSet(entry.FontDeclined, []string{"Hack-Bold.ttf"}) {
		t.Errorf("font_declined lost: %v", entry.FontDeclined)
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
