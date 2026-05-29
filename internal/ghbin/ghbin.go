package ghbin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Find resolves the gh CLI binary: checks PATH first, then the ghpm-managed
// copy at ~/.ghpm/bin/gh.
func Find() (string, error) {
	if p, err := exec.LookPath("gh"); err == nil {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err == nil {
		managed := filepath.Join(home, ".ghpm", "bin", "gh")
		if _, err := os.Stat(managed); err == nil {
			return managed, nil
		}
	}
	return "", fmt.Errorf("gh CLI not found — install it from https://cli.github.com/")
}
