package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/meop/ghpm/internal/store"
)

// PackageEntry records one installed package. Its selected release assets are
// overlaid (in Assets order, later wins on path collision) into a single extract
// dir, so bins and fonts are tracked as flat maps over that combined tree rather
// than per asset. Assets is the ordered list of release asset names, kept so sync
// can re-download and re-overlay the same set.
//
// BinDeclined and FontDeclined record the discovered artifacts the user did *not*
// select. Together with the selected Bin/Font maps they reconstruct the full set
// discovered at install time, which is what sync compares against the freshly
// discovered set: an identical set means the package's layout is unchanged and the
// prior choices carry over silently; any difference means the layout changed and
// the package is reprompted from scratch. Without recording the declines, sync
// could not tell "user only wanted a subset" apart from "the release gained new
// binaries", so it would reprompt on every run.
type PackageEntry struct {
	Pin          string            `json:"pin"`
	Version      string            `json:"version"`
	Assets       []string          `json:"assets,omitempty"`
	Bin          map[string]string `json:"bin,omitempty"`
	Font         map[string]string `json:"font,omitempty"`
	BinDeclined  []string          `json:"bin_declined,omitempty"`
	FontDeclined []string          `json:"font_declined,omitempty"`
}

// AllBins returns the package's shimName → binKey map.
func (p PackageEntry) AllBins() map[string]string { return p.Bin }

// AllFonts returns the package's fontName → fontPath map.
func (p PackageEntry) AllFonts() map[string]string { return p.Font }

// DiscoveredBins returns every bin key discovered at install time — the selected
// ones (Bin values) plus the declined ones. A legacy entry written before declines
// were tracked has no BinDeclined, so this returns only the selected keys; sync
// will then reprompt that package once and store the full set going forward.
func (p PackageEntry) DiscoveredBins() []string {
	keys := make([]string, 0, len(p.Bin)+len(p.BinDeclined))
	for _, binKey := range p.Bin {
		keys = append(keys, binKey)
	}
	return append(keys, p.BinDeclined...)
}

// DiscoveredFonts returns every font path discovered at install time — selected
// (Font values) plus declined — mirroring DiscoveredBins.
func (p PackageEntry) DiscoveredFonts() []string {
	paths := make([]string, 0, len(p.Font)+len(p.FontDeclined))
	for _, fontPath := range p.Font {
		paths = append(paths, fontPath)
	}
	return append(paths, p.FontDeclined...)
}

type Manifest struct {
	Repos    map[string]string       `json:"repo"`
	Extracts map[string]PackageEntry `json:"extract"`
}

func (m *Manifest) AddExtract(key string, entry PackageEntry, source string) {
	baseName, _, _ := ParseVersionSuffix(key)
	if m.Repos == nil {
		m.Repos = map[string]string{}
	}
	if m.Extracts == nil {
		m.Extracts = map[string]PackageEntry{}
	}
	m.Repos[baseName] = source
	m.Extracts[key] = entry
}

func (m *Manifest) RemoveExtract(key string) {
	delete(m.Extracts, key)
	baseName, _, _ := ParseVersionSuffix(key)
	for k := range m.Extracts {
		if n, _, _ := ParseVersionSuffix(k); n == baseName {
			return
		}
	}
	delete(m.Repos, baseName)
}

func manifestPath() (string, error) {
	dir, err := store.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "manifest.json"), nil
}

func LoadManifest() (*Manifest, error) {
	path, err := manifestPath()
	if err != nil {
		return nil, err
	}
	return loadManifestFile(path)
}

func SaveManifest(m *Manifest) error {
	path, err := manifestPath()
	if err != nil {
		return err
	}
	return saveManifestFile(m, path)
}

func loadManifestFile(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Manifest{
			Repos:    map[string]string{},
			Extracts: map[string]PackageEntry{},
		}, nil
	}
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m.Repos == nil {
		m.Repos = map[string]string{}
	}
	if m.Extracts == nil {
		m.Extracts = map[string]PackageEntry{}
	}
	return &m, nil
}

func saveManifestFile(m *Manifest, path string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
