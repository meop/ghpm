package shim

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/meop/ghpm/internal/store"
)

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// Create stamps a sheesh shim at ~/.ghpm/bin/shimName that execs the resolved
// binary inside pkgDir/binSubdir when run. kebab selects the appropriate sheesh
// template (console vs GUI on Windows) automatically.
func Create(shimName, binaryName, pkgDir, binSubdir string) error {
	shimDir, err := store.ShimDir()
	if err != nil {
		return err
	}
	kebabPath := filepath.Join(shimDir, exeName("kebab"))
	if _, err := os.Stat(kebabPath); err != nil {
		return fmt.Errorf("kebab not found at %s — run 'ghpm upgrade' to install sheesh", kebabPath)
	}

	binDir, err := store.BinDir()
	if err != nil {
		return err
	}

	source := filepath.Join(pkgDir, binSubdir, binaryName)
	target := filepath.Join(binDir, shimName)

	// On Windows, the loader opens .exe files with FILE_SHARE_DELETE, so Remove
	// succeeds via delete-on-close even on a running shim, freeing the path for kebab.
	if runtime.GOOS == "windows" {
		_ = os.Remove(target)
	}
	return exec.Command(kebabPath, "--source-path", source, "--target-path", target).Run()
}

// Remove deletes the shim for shimName from ~/.ghpm/bin/.
func Remove(shimName string) error {
	binDir, err := store.BinDir()
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(binDir, shimName))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
