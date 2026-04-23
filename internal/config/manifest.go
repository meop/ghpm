package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type PackageEntry struct {
	Source       string `json:"source"`
	Version      string `json:"version"`
	Versioned    bool   `json:"versioned"`
	AssetPattern string `json:"asset_pattern"`
	BinaryName   string `json:"binary_name"`
	InstalledAt  string `json:"installed_at"`
}

type Manifest struct {
	Packages map[string]PackageEntry `json:"packages"`
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
		return &Manifest{Packages: map[string]PackageEntry{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m.Packages == nil {
		m.Packages = map[string]PackageEntry{}
	}
	return &m, nil
}

func saveManifestFile(m *Manifest, path string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func Now() string {
	return time.Now().UTC().Format(time.RFC3339)
}
