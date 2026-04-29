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
	return filepath.Join(base, "bin"), nil
}

func ExtractsDir() (string, error) {
	base, err := ghpmDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "extracts"), nil
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

func ScriptsDir() (string, error) {
	base, err := ghpmDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "scripts")
	return dir, os.MkdirAll(dir, 0755)
}

func ReleaseBaseDir() (string, error) {
	base, err := ghpmDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "releases"), nil
}

func ReposBaseDir() (string, error) {
	base, err := ghpmDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "repos"), nil
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
	dir := filepath.Join(base, relPath, version)
	return dir, os.MkdirAll(dir, 0755)
}
