package store

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/meop/golie/home"
	"github.com/meop/golie/layout"
)

// Dirs abstracts the ~/.ghpm directory layout so commands can be tested with a
// fake filesystem. LocalDirs is the production implementation.
type Dirs interface {
	BinDir() (string, error)
	ShimDir() (string, error)
	ExtractsDir() (string, error)
	ExtractDir(key string) (string, error)
	LatestDir(key string) (string, error)
	VersionDir(key, version string) (string, error)
	StageDir(key, version string) (string, error)
	ReleaseDir(source, version string) (string, error)
	ReleaseBaseDir() (string, error)
	RepoDir(source string) (string, error)
	ReposBaseDir() (string, error)
}

type LocalDirs struct{}

const testHomeEnv = "GHPM_TEST_HOME"

func NewLocalDirs() *LocalDirs { return &LocalDirs{} }

func (*LocalDirs) BinDir() (string, error)               { return BinDir() }
func (*LocalDirs) ShimDir() (string, error)              { return ShimDir() }
func (*LocalDirs) ExtractsDir() (string, error)          { return ExtractsDir() }
func (*LocalDirs) ExtractDir(key string) (string, error) { return ExtractDir(key) }
func (*LocalDirs) LatestDir(key string) (string, error)  { return LatestDir(key) }
func (*LocalDirs) VersionDir(key, version string) (string, error) {
	return VersionDir(key, version)
}
func (*LocalDirs) StageDir(key, version string) (string, error) { return StageDir(key, version) }
func (*LocalDirs) ReleaseDir(source, version string) (string, error) {
	return ReleaseDir(source, version)
}
func (*LocalDirs) ReleaseBaseDir() (string, error) { return ReleaseBaseDir() }
func (*LocalDirs) RepoDir(source string) (string, error) {
	return RepoDir(source)
}
func (*LocalDirs) ReposBaseDir() (string, error) { return ReposBaseDir() }

// UserHomeDir returns the user's real home in production and an explicitly
// isolated home in tests. Test binaries refuse to fall back to the real home:
// a missed test setup must fail before it can modify ~/.ghpm or user fonts.
// The guard itself is golie's.
func UserHomeDir() (string, error) { return home.Dir(testHomeEnv) }

// UsingTestHome reports whether filesystem access is redirected to a test
// home. Callers with OS-specific paths, such as LOCALAPPDATA, use this to keep
// every derived path under the same isolated root.
func UsingTestHome() bool { return home.UsingTest(testHomeEnv) }

// Layout is ~/.ghpm as golie lays a state directory out: bin, vendor,
// extract and download, with the manifest and the lock beside them. Only
// repo/ below is ghpm's own.
func Layout() (layout.Layout, error) {
	home, err := UserHomeDir()
	if err != nil {
		return layout.Layout{}, err
	}
	return layout.Layout{Root: filepath.Join(home, ".ghpm")}, nil
}

// Dir returns the root ~/.ghpm directory path without creating it.
func Dir() (string, error) {
	l, err := Layout()
	return l.Root, err
}

func ghpmSubDir(mkdir bool, elems ...string) (string, error) {
	base, err := Dir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(append([]string{base}, elems...)...)
	if mkdir {
		return dir, os.MkdirAll(dir, 0755)
	}
	return dir, nil
}

// BinDir holds ghpm itself and the shims it stamps for packages — everything
// meant to be on the user's PATH.
func BinDir() (string, error) {
	l, err := Layout()
	if err != nil {
		return "", err
	}
	return l.BinDir()
}

// VendorDir holds the tools ghpm needs to work, not the ones it installs for
// you. Nothing here belongs on PATH: it is invoked by absolute path, so a
// vendored copy can be older, newer, or differently authenticated than the
// same tool elsewhere on the system without either one disturbing the other.
func VendorDir() (string, error) {
	l, err := Layout()
	if err != nil {
		return "", err
	}
	return l.VendorDir()
}

// ShimDir holds wedge's own mallet stamper (and whatever else its release
// ships), alongside gh under vendor — the other tool ghpm needs to work
// rather than one it installs for you.
func ShimDir() (string, error) {
	l, err := Layout()
	if err != nil {
		return "", err
	}
	return l.ShimDir()
}

func ExtractsDir() (string, error) {
	l, err := Layout()
	return l.ExtractsDir(), err
}

// ExtractDir holds every version of key and its latest pointer.
func ExtractDir(key string) (string, error) {
	l, err := Layout()
	return l.ExtractPath(key), err
}

// LatestDir is key's pointer to its installed version, which shims are stamped through.
func LatestDir(key string) (string, error) {
	l, err := Layout()
	return l.LatestPath(key), err
}

// VersionDir is where one version of key is unpacked.
func VersionDir(key, version string) (string, error) {
	l, err := Layout()
	return l.VersionPath(key, version), err
}

// StageDir is where a version of key is unpacked before it is placed.
func StageDir(key, version string) (string, error) {
	l, err := Layout()
	return l.StagePath(key, version), err
}

func ReleaseBaseDir() (string, error) {
	l, err := Layout()
	return l.DownloadsDir(), err
}

func ReposBaseDir() (string, error) {
	return ghpmSubDir(false, "repo")
}

// SourceToRelPath converts a source URI (e.g. "github.com/owner/repo") to a
// relative filesystem path using the OS path separator.
func SourceToRelPath(source string) string { return layout.SourceToRelPath(source) }

func RepoDir(source string) (string, error) {
	base, err := ReposBaseDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, SourceToRelPath(source))
	return dir, os.MkdirAll(dir, 0755)
}

func ReleaseDir(source, ver string) (string, error) {
	l, err := Layout()
	if err != nil {
		return "", err
	}
	return l.DownloadDir(source, ver)
}

func SourceFromPath(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 3 {
		return ""
	}
	return parts[0] + "/" + parts[1] + "/" + parts[2]
}
