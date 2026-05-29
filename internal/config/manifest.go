package config

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
)

type AssetEntry struct {
	Bin  map[string]string `json:"bin,omitempty"`
	Font map[string]string `json:"font,omitempty"`
}

func (ae AssetEntry) IsBin() bool  { return len(ae.Bin) > 0 }
func (ae AssetEntry) IsFont() bool { return len(ae.Font) > 0 }

type PackageEntry struct {
	Pin     string                `json:"pin"`
	Version string                `json:"version"`
	Asset   map[string]AssetEntry `json:"asset,omitempty"`
}

// BinAssetName returns the name of the first asset that contains bins, or empty string.
func (p PackageEntry) BinAssetName() string {
	for name, ae := range p.Asset {
		if ae.IsBin() {
			return name
		}
	}
	return ""
}

// AllBins returns a merged shimName → binKey map across all assets.
func (p PackageEntry) AllBins() map[string]string {
	result := map[string]string{}
	for _, ae := range p.Asset {
		maps.Copy(result, ae.Bin)
	}
	return result
}

// AllFonts returns a merged fontKey → fontKey map across all assets.
func (p PackageEntry) AllFonts() map[string]string {
	result := map[string]string{}
	for _, ae := range p.Asset {
		maps.Copy(result, ae.Font)
	}
	return result
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

func ghpmDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ghpm"), nil
}

func manifestPath() (string, error) {
	dir, err := ghpmDir()
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

// rawPackageEntry handles both legacy format (asset string + bin map) and new format (asset map).
type rawPackageEntry struct {
	Pin     string            `json:"pin"`
	Version string            `json:"version"`
	Asset   json.RawMessage   `json:"asset,omitempty"` // string (legacy) or map[string]AssetEntry (new)
	Bin     map[string]string `json:"bin,omitempty"`   // legacy
}

type rawManifest struct {
	Repos    map[string]string          `json:"repo"`
	Extracts map[string]rawPackageEntry `json:"extract"`
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
	var raw rawManifest
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	m := &Manifest{
		Repos:    raw.Repos,
		Extracts: make(map[string]PackageEntry, len(raw.Extracts)),
	}
	if m.Repos == nil {
		m.Repos = map[string]string{}
	}
	for k, v := range raw.Extracts {
		entry := PackageEntry{Pin: v.Pin, Version: v.Version}
		if len(v.Asset) > 0 {
			var assetMap map[string]AssetEntry
			if json.Unmarshal(v.Asset, &assetMap) == nil {
				entry.Asset = assetMap
			} else {
				var assetStr string
				if json.Unmarshal(v.Asset, &assetStr) == nil && assetStr != "" {
					entry.Asset = map[string]AssetEntry{
						assetStr: {Bin: v.Bin},
					}
				}
			}
		}
		if entry.Asset == nil {
			entry.Asset = map[string]AssetEntry{}
		}
		m.Extracts[k] = entry
	}
	return m, nil
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
