package config

import (
	"github.com/meop/golie/manifest"

	"github.com/meop/ghpm/internal/store"
)

// The manifest is golie's: ghpm and gotm keep the same file, so what reads it
// (list, sync, remove, tidy) reads one contract. See golie's manifest package
// for what an entry records and why.
type (
	PackageEntry = manifest.Entry
	Manifest     = manifest.Manifest
)

func manifestPath() (string, error) {
	l, err := store.Layout()
	if err != nil {
		return "", err
	}
	return l.ManifestPath(), nil
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

func loadManifestFile(path string) (*Manifest, error) { return manifest.Load(path) }

func saveManifestFile(m *Manifest, path string) error { return manifest.Save(m, path) }
