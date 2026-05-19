package store

import (
	"os"
	"path/filepath"
	"strings"
)

func ghpmDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ghpm"), nil
}

func BinDir() (string, error) {
	base, err := ghpmDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "bin")
	return dir, os.MkdirAll(dir, 0755)
}

func ExtractsDir() (string, error) {
	base, err := ghpmDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "extract"), nil
}

func ExtractBaseDir(key string) (string, error) {
	base, err := ExtractsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, key), nil
}

func ExtractDir(key, version string) (string, error) {
	base, err := ExtractBaseDir(key)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, version)
	return dir, os.MkdirAll(dir, 0755)
}

func ReleaseBaseDir() (string, error) {
	base, err := ghpmDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "download"), nil
}

func ReposBaseDir() (string, error) {
	base, err := ghpmDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "repo"), nil
}

func RepoDir(source string) (string, error) {
	base, err := ReposBaseDir()
	if err != nil {
		return "", err
	}
	relPath := strings.ReplaceAll(source, "/", string(filepath.Separator))
	dir := filepath.Join(base, relPath)
	return dir, os.MkdirAll(dir, 0755)
}

func ReleaseDir(source, version string) (string, error) {
	base, err := ReleaseBaseDir()
	if err != nil {
		return "", err
	}
	relPath := strings.ReplaceAll(source, "/", string(filepath.Separator))
	dir := filepath.Join(base, relPath, normalizeVersion(version))
	return dir, os.MkdirAll(dir, 0755)
}

func normalizeVersion(v string) string {
	for i, r := range v {
		if r >= '0' && r <= '9' {
			return v[i:]
		}
	}
	return v
}
