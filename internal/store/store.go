package store

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/meop/ghpm/internal/version"
)

// Dirs abstracts the ~/.ghpm directory layout so commands can be tested with a
// fake filesystem. LocalDirs is the production implementation.
type Dirs interface {
	BinDir() (string, error)
	ShimDir() (string, error)
	ExtractsDir() (string, error)
	ExtractBaseDir(key string) (string, error)
	ExtractDir(key, version, assetName string) (string, error)
	ReleaseDir(source, version string) (string, error)
	ReleaseBaseDir() (string, error)
	RepoDir(source string) (string, error)
	ReposBaseDir() (string, error)
}

type LocalDirs struct{}

func NewLocalDirs() *LocalDirs { return &LocalDirs{} }

func (*LocalDirs) BinDir() (string, error)      { return BinDir() }
func (*LocalDirs) ShimDir() (string, error)     { return ShimDir() }
func (*LocalDirs) ExtractsDir() (string, error) { return ExtractsDir() }
func (*LocalDirs) ExtractBaseDir(k string) (string, error) {
	return ExtractBaseDir(k)
}
func (*LocalDirs) ExtractDir(key, version, assetName string) (string, error) {
	return ExtractDir(key, version, assetName)
}
func (*LocalDirs) ReleaseDir(source, version string) (string, error) {
	return ReleaseDir(source, version)
}
func (*LocalDirs) ReleaseBaseDir() (string, error) { return ReleaseBaseDir() }
func (*LocalDirs) RepoDir(source string) (string, error) {
	return RepoDir(source)
}
func (*LocalDirs) ReposBaseDir() (string, error) { return ReposBaseDir() }

func ghpmDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ghpm"), nil
}

// Dir returns the root ~/.ghpm directory path without creating it.
func Dir() (string, error) {
	return ghpmDir()
}

func ghpmSubDir(mkdir bool, elems ...string) (string, error) {
	base, err := ghpmDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(append([]string{base}, elems...)...)
	if mkdir {
		return dir, os.MkdirAll(dir, 0755)
	}
	return dir, nil
}

func BinDir() (string, error) {
	return ghpmSubDir(true, "bin")
}

func ShimDir() (string, error) {
	return ghpmSubDir(true, "shim")
}

func ExtractsDir() (string, error) {
	return ghpmSubDir(false, "extract")
}

func ExtractBaseDir(key string) (string, error) {
	base, err := ExtractsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, key), nil
}

func ExtractDir(key, version, assetName string) (string, error) {
	base, err := ExtractBaseDir(key)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, version, assetName)
	return dir, os.MkdirAll(dir, 0755)
}

func ReleaseBaseDir() (string, error) {
	return ghpmSubDir(false, "download")
}

func ReposBaseDir() (string, error) {
	return ghpmSubDir(false, "repo")
}

// SourceToRelPath converts a source URI (e.g. "github.com/owner/repo") to a
// relative filesystem path using the OS path separator.
func SourceToRelPath(source string) string {
	return strings.ReplaceAll(source, "/", string(filepath.Separator))
}

func RepoDir(source string) (string, error) {
	base, err := ReposBaseDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, SourceToRelPath(source))
	return dir, os.MkdirAll(dir, 0755)
}

func ReleaseDir(source, ver string) (string, error) {
	base, err := ReleaseBaseDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, SourceToRelPath(source), version.Normalize(ver))
	return dir, os.MkdirAll(dir, 0755)
}

func SourceFromPath(rel string) string {
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 3 {
		return ""
	}
	return parts[0] + "/" + parts[1] + "/" + parts[2]
}
