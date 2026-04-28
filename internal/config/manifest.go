package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type PackageEntry struct {
	Pin        string              `json:"pin"`
	Version    string              `json:"version"`
	Asset      string              `json:"asset"`
	Paths      map[string][]string `json:"paths,omitempty"`
	BinaryName string              `json:"binary_name,omitempty"`
}

type Manifest struct {
	Repos    map[string]string       `json:"repos"`
	Installs map[string]PackageEntry `json:"installs"`
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
			Installs: map[string]PackageEntry{},
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
	if m.Installs == nil {
		m.Installs = map[string]PackageEntry{}
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
