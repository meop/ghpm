package ghbin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/meop/ghpm/internal/store"
)

// vendorName is the vendored gh binary's filename — must match what
// `ghpm upgrade` writes, or the vendored copy is unfindable on Windows.
func vendorName() string {
	if runtime.GOOS == "windows" {
		return "gh.exe"
	}
	return "gh"
}

// vendorSubDir returns ghpm's own gh subtree under vendor: vendor/gh/, next
// to vendor/sheesh/ (see internal/store.ShimDir).
func vendorSubDir() (string, error) {
	dir, err := store.VendorDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gh"), nil
}

// VendorPath is where ghpm keeps its own gh: under vendor, off PATH, so it is
// never the gh anything else on the system reaches for.
func VendorPath() (string, error) {
	dir, err := vendorSubDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, vendorName()), nil
}

// ConfigDir is the vendored gh's own GH_CONFIG_DIR. Its auth is ghpm's, not
// the user's: logging the system gh in or out does not change what ghpm can
// reach, and ghpm's token is not handed to every other tool that shells out
// to gh.
func ConfigDir() (string, error) {
	dir, err := vendorSubDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gh-config"), nil
}

// Find resolves ghpm's own vendored gh — the only gh ghpm ever runs
// internally. It never falls back to PATH: what the system otherwise has
// installed as gh is not ghpm's to depend on or authenticate as. Ensure is
// what makes sure this succeeds.
func Find() (string, error) {
	p, err := VendorPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("gh not vendored yet at %s", p)
	}
	return p, nil
}

// Command builds an invocation of ghpm's vendored gh, against its own
// GH_CONFIG_DIR so its auth is independent of the system gh's.
func Command(args ...string) (*exec.Cmd, error) {
	path, err := Find()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path, args...) //nolint:gosec
	cfgDir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		return nil, err
	}
	cmd.Env = append(os.Environ(), "GH_CONFIG_DIR="+cfgDir)
	return cmd, nil
}
