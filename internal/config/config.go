package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/meop/ghpm/internal/store"
)

type Settings struct {
	CacheTTL      string            `toml:"cache_ttl"`
	Color         map[string]string `toml:"color"`
	NoColor       bool              `toml:"no_color"`
	NumParallel   int               `toml:"num_parallel"`
	RepoSources   []string          `toml:"repo_sources"`
	SkipHashCheck bool              `toml:"skip_hash_check"`
}

func defaultSettings() *Settings {
	return &Settings{
		CacheTTL: "5m",
		Color: map[string]string{
			"fail": "red",
			"info": "blue",
			"new":  "cyan",
			"old":  "magenta",
			"pass": "green",
			"warn": "yellow",
		},
		NoColor:       false,
		NumParallel:   5,
		RepoSources:   []string{RepoGhpmConfig.URI},
		SkipHashCheck: false,
	}
}

func LoadSettings() (*Settings, error) {
	dir, err := store.Dir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "config.toml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return defaultSettings(), nil
	}
	if err != nil {
		return nil, err
	}
	s := defaultSettings()
	if err := toml.Unmarshal(data, s); err != nil {
		return nil, err
	}
	return s, nil
}

func EnsureDirs() error {
	base, err := store.Dir()
	if err != nil {
		return err
	}
	for _, dir := range []string{
		filepath.Join(base, "bin"),
		filepath.Join(base, "extract"),
		filepath.Join(base, "download"),
		filepath.Join(base, "repo"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}
