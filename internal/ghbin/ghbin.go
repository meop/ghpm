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

// VendorPath is where ghpm keeps its own gh: under vendor, off PATH, so it is
// never the gh anything else on the system reaches for.
func VendorPath() (string, error) {
	dir, err := store.VendorDir()
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
	dir, err := store.VendorDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gh-config"), nil
}

// Find resolves the gh CLI binary, preferring ghpm's vendored copy so ghpm's
// behaviour does not change when the system gh does. PATH is the fallback,
// which is also the bootstrap: a fresh install has no vendored gh yet, and
// needs one to fetch it.
func Find() (string, error) {
	if p, err := VendorPath(); err == nil {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if p, err := exec.LookPath("gh"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("gh CLI not found — install it from https://cli.github.com/")
}

// Command builds a gh invocation. The vendored copy runs against ghpm's own
// config dir; a gh found on PATH is the user's, and runs with the user's
// config, since sending it at an empty config dir would only ask them to log
// in again to a binary they did not choose.
func Command(args ...string) (*exec.Cmd, error) {
	path, err := Find()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path, args...) //nolint:gosec
	if vendored, err := VendorPath(); err == nil && path == vendored {
		if cfgDir, err := ConfigDir(); err == nil {
			if err := os.MkdirAll(cfgDir, 0755); err == nil {
				cmd.Env = append(os.Environ(), "GH_CONFIG_DIR="+cfgDir)
			}
		}
	}
	return cmd, nil
}
