package shim

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/meop/ghpm/internal/store"
)

// Create installs a shim named shimName (the manifest key, e.g. "fzf" or "fzf@0.58")
// pointing at binaryName inside pkgDir/binSubdir.
// On Unix this is a symlink; on Windows a .cmd wrapper.
func Create(shimName, binaryName, pkgDir, binSubdir string) error {
	binDir, err := store.BinDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		target := filepath.Join(pkgDir, binSubdir, binaryName+".exe")
		shimPath := filepath.Join(binDir, shimName+".cmd")
		content := fmt.Sprintf("@\"%s\" %%*\r\n", target)
		return os.WriteFile(shimPath, []byte(content), 0644)
	}

	target := filepath.Join(pkgDir, binSubdir, binaryName)
	shimPath := filepath.Join(binDir, shimName)
	_ = os.Remove(shimPath)
	return os.Symlink(target, shimPath)
}

// Remove deletes the shim for shimName from ~/.ghpm/bin/.
func Remove(shimName string) error {
	binDir, err := store.BinDir()
	if err != nil {
		return err
	}
	var err2 error
	if runtime.GOOS == "windows" {
		err2 = os.Remove(filepath.Join(binDir, shimName+".cmd"))
	} else {
		err2 = os.Remove(filepath.Join(binDir, shimName))
	}
	if os.IsNotExist(err2) {
		return nil
	}
	return err2
}
