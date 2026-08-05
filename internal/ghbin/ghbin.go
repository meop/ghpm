package ghbin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/meop/ghpm/internal/store"
)

// managedName is the ghpm-managed gh binary's filename — must match what
// `ghpm upgrade` actually writes (internal/cli's exeName(binGh)), or the
// managed copy is unfindable on Windows once installed.
func managedName() string {
	if runtime.GOOS == "windows" {
		return "gh.exe"
	}
	return "gh"
}

// Find resolves the gh CLI binary: checks PATH first, then the ghpm-managed
// copy at ~/.ghpm/bin/gh.
func Find() (string, error) {
	if p, err := exec.LookPath("gh"); err == nil {
		return p, nil
	}
	if dir, err := store.Dir(); err == nil {
		managed := filepath.Join(dir, "bin", managedName())
		if _, err := os.Stat(managed); err == nil {
			return managed, nil
		}
	}
	return "", fmt.Errorf("gh CLI not found — install it from https://cli.github.com/")
}
