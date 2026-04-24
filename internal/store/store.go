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

// BinDir returns ~/.ghpm/bin.
func BinDir() (string, error) {
	base, err := ghpmDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "bin"), nil
}

// ReleaseBaseDir returns ~/.ghpm/releases.
func ReleaseBaseDir() (string, error) {
	base, err := ghpmDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "releases"), nil
}

// ReposBaseDir returns ~/.ghpm/repos.
func ReposBaseDir() (string, error) {
	base, err := ghpmDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "repos"), nil
}

// RepoDir returns (and creates) the cache directory for a specific repo source.
// source is e.g. "github.com/meop/ghpm-config".
func RepoDir(source string) (string, error) {
	base, err := ReposBaseDir()
	if err != nil {
		return "", err
	}
	relPath := strings.ReplaceAll(source, "/", string(filepath.Separator))
	dir := filepath.Join(base, relPath)
	return dir, os.MkdirAll(dir, 0755)
}

// ReleaseDir returns the cache directory for a specific source+version.
// source is e.g. "github.com/junegunn/fzf", version is e.g. "v0.56.0".
func ReleaseDir(source, version string) (string, error) {
	base, err := ReleaseBaseDir()
	if err != nil {
		return "", err
	}
	relPath := strings.ReplaceAll(source, "/", string(filepath.Separator))
	dir := filepath.Join(base, relPath, version)
	return dir, os.MkdirAll(dir, 0755)
}
