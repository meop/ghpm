package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type PackageEntry struct {
	Pin       string            `json:"pin"`
	Version   string            `json:"version"`
	AssetName string            `json:"asset_name"`
	BinDir    string            `json:"bin_dir,omitempty"`
	Bins      map[string]string `json:"bins,omitempty"` // shim name → relative binary path within extract
}

type Manifest struct {
	Repos    map[string]string       `json:"repo"`
	Extracts map[string]PackageEntry `json:"extract"`
}

func HomeDir() (string, error) {
	return os.UserHomeDir()
}

func manifestPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ghpm", "manifest.json"), nil
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
	if len(m.Repos) == 0 || len(m.Extracts) == 0 {
		var legacy struct {
			Repos    map[string]string       `json:"repos"`
			Extracts map[string]PackageEntry `json:"extracts"`
		}
		if json.Unmarshal(data, &legacy) == nil {
			if len(m.Repos) == 0 && len(legacy.Repos) > 0 {
				m.Repos = legacy.Repos
			}
			if len(m.Extracts) == 0 && len(legacy.Extracts) > 0 {
				m.Extracts = legacy.Extracts
			}
		}
	}
	if m.Repos == nil {
		m.Repos = map[string]string{}
	}
	if m.Extracts == nil {
		m.Extracts = map[string]PackageEntry{}
	}
	// Migrate legacy bin_name (singular) and bin_names ([]string) → bins (map)
	var legacyBins struct {
		Extracts map[string]struct {
			BinName  string   `json:"bin_name,omitempty"`
			BinNames []string `json:"bin_names,omitempty"`
		} `json:"extract"`
	}
	if json.Unmarshal(data, &legacyBins) == nil {
		for k, e := range legacyBins.Extracts {
			entry, ok := m.Extracts[k]
			if !ok || len(entry.Bins) > 0 {
				continue
			}
			bins := make(map[string]string)
			for _, bn := range e.BinNames {
				bins[bn] = bn // shimName=bn → binKey=bn (default: shim name == binary filename)
			}
			if e.BinName != "" && len(bins) == 0 {
				bins[e.BinName] = e.BinName
			}
			if len(bins) > 0 {
				entry.Bins = bins
				m.Extracts[k] = entry
			}
		}
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
	return os.Rename(tmp, path)
}
