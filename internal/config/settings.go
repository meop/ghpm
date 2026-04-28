package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type PlatPriority map[string][]string

type Settings struct {
	CacheTTL     string            `json:"cache_ttl"`
	Color        map[string]string `json:"color"`
	NoColor      bool              `json:"no_color"`
	NoVerify     bool              `json:"no_verify"`
	NumParallel  int               `json:"num_parallel"`
	PlatPriority PlatPriority      `json:"plat_priority"`
	RepoSources  []string          `json:"repo_sources"`
}

func defaultSettings() *Settings {
	return &Settings{
		Color: map[string]string{
			"fail": "red",
			"info": "blue",
			"new":  "cyan",
			"old":  "magenta",
			"pass": "green",
			"warn": "yellow",
		},
		NoColor:     false,
		NoVerify:    false,
		NumParallel: 5,
		CacheTTL:    "5m",
		PlatPriority: PlatPriority{
			"linux":   {"gnu", "musl"},
			"windows": {"msvc", "gnu"},
		},
		RepoSources: []string{"github.com/meop/ghpm-config"},
	}
}

func LoadSettings() (*Settings, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".ghpm", "settings.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return defaultSettings(), nil
	}
	if err != nil {
		return nil, err
	}
	s := defaultSettings()
	if err := json.Unmarshal(data, s); err != nil {
		return nil, err
	}
	return s, nil
}

func EnsureDirs() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	base := filepath.Join(home, ".ghpm")
	for _, dir := range []string{
		filepath.Join(base, "bin"),
		filepath.Join(base, "packages"),
		filepath.Join(base, "releases"),
		filepath.Join(base, "repos"),
		filepath.Join(base, "scripts"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}
