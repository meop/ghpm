package shim

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/meop/ghpm/internal/store"
)

// sheeshMarker is present in every kebab template, stamped or not. A file
// carrying it and pointing into ghpm's extract dir is a shim ghpm created;
// anything else in the directory belongs to somebody else.
const sheeshMarker = "use kebab to stamp a source path"

// IsShim reports whether path is a shim ghpm stamped. It is the guard for every
// write and delete: the bin directory may be shared with binaries ghpm knows
// nothing about, and those are not ghpm's to replace or remove.
func IsShim(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > 8<<20 {
		return false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if !bytes.Contains(body, []byte(sheeshMarker)) {
		return false
	}
	extracts, err := store.ExtractsDir()
	if err != nil {
		return false
	}
	return bytes.Contains(body, []byte(extracts))
}

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// Create stamps a sheesh shim at ~/.ghpm/bin/shimName that execs the resolved
// binary inside pkgDir/binSubdir when run. kebab selects the appropriate sheesh
// template (console vs GUI on Windows) automatically.
func Create(shimName, binaryName, pkgDir, binSubdir string, force bool) error {
	binDir, err := store.BinDir()
	if err != nil {
		return err
	}
	target := filepath.Join(binDir, shimName)

	// checked before anything else: whether ghpm may write here does not depend
	// on ghpm's own tooling being installed
	if _, err := os.Stat(target); err == nil && !IsShim(target) && !force {
		return fmt.Errorf("%s exists and is not a ghpm shim — pass --force to replace it", target)
	}

	shimDir, err := store.ShimDir()
	if err != nil {
		return err
	}
	kebabPath := filepath.Join(shimDir, exeName("kebab"))
	if _, err := os.Stat(kebabPath); err != nil {
		return fmt.Errorf("kebab not found at %s — run 'ghpm upgrade' to install sheesh", kebabPath)
	}

	source := filepath.Join(pkgDir, binSubdir, binaryName)

	// On Windows, the loader opens .exe files with FILE_SHARE_DELETE, so Remove
	// succeeds via delete-on-close even on a running shim, freeing the path for kebab.
	if runtime.GOOS == "windows" {
		_ = os.Remove(target)
	}
	return exec.Command(kebabPath, "--source-path", source, "--target-path", target).Run()
}

// Remove deletes the shim for shimName from ~/.ghpm/bin/. A file of that name
// that ghpm did not stamp is left alone: ghpm removing its own package should
// never take somebody else's binary with it.
func Remove(shimName string) error {
	binDir, err := store.BinDir()
	if err != nil {
		return err
	}
	target := filepath.Join(binDir, shimName)
	if _, err := os.Stat(target); os.IsNotExist(err) {
		return nil
	}
	if !IsShim(target) {
		return nil
	}
	err = os.Remove(target)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
