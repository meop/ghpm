package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Settings struct {
	CacheTTL    string            `json:"cache_ttl"`
	Color       map[string]string `json:"color"`
	NoColor     bool              `json:"no_color"`
	NoVerify    bool              `json:"no_verify"`
	NumParallel int               `json:"num_parallel"`
	RepoSources []string          `json:"repo_sources"`
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
		RepoSources: []string{RepoGhpmConfig.URI},
	}
}

func LoadSettings() (*Settings, error) {
	dir, err := ghpmDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "settings.json")
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
	base, err := ghpmDir()
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
