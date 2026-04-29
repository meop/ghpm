package shim

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/meop/ghpm/internal/store"
)

// Create installs a shim for name pointing at the binary inside pkgDir/binSubdir.
// On Unix this is a symlink; on Windows a .cmd wrapper.
func Create(name, pkgDir, binSubdir string) error {
	binDir, err := store.BinDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		target := filepath.Join(pkgDir, binSubdir, name+".exe")
		shimPath := filepath.Join(binDir, name+".cmd")
		content := fmt.Sprintf("@\"%s\" %%*\r\n", target)
		return os.WriteFile(shimPath, []byte(content), 0644)
	}

	target := filepath.Join(pkgDir, binSubdir, name)
	shimPath := filepath.Join(binDir, name)
	_ = os.Remove(shimPath)
	return os.Symlink(target, shimPath)
}

// Remove deletes the shim for name from ~/.ghpm/bin/.
func Remove(name string) error {
	binDir, err := store.BinDir()
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		err = os.Remove(filepath.Join(binDir, name+".cmd"))
	} else {
		err = os.Remove(filepath.Join(binDir, name))
	}
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
