package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type PlatformPriority map[string][]string

type Settings struct {
	Parallelism      int              `json:"parallelism"`
	PlatformPriority PlatformPriority `json:"platform_priority"`
	NoVerify         bool             `json:"no_verify"`
	ToolRepos        []string         `json:"tool_repos"`
}

func defaultSettings() *Settings {
	return &Settings{
		Parallelism: 5,
		PlatformPriority: PlatformPriority{
			"linux":   {"gnu", "musl"},
			"windows": {"msvc", "gnu"},
		},
		ToolRepos: []string{"github.com/meop/ghpm-config"},
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
		filepath.Join(base, "releases"),
		filepath.Join(base, "tools"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}
