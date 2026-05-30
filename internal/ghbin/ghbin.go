package ghbin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/meop/ghpm/internal/store"
)

// Find resolves the gh CLI binary: checks PATH first, then the ghpm-managed
// copy at ~/.ghpm/bin/gh.
func Find() (string, error) {
	if p, err := exec.LookPath("gh"); err == nil {
		return p, nil
	}
	if dir, err := store.Dir(); err == nil {
		managed := filepath.Join(dir, "bin", "gh")
		if _, err := os.Stat(managed); err == nil {
			return managed, nil
		}
	}
	return "", fmt.Errorf("gh CLI not found — install it from https://cli.github.com/")
}
