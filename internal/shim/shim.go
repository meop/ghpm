// Package shim stamps and removes the shims in ~/.ghpm/bin. The stamping
// itself is golie's; this names where ghpm's shims live and what they may
// point at.
package shim

import (
	"path/filepath"

	golie "github.com/meop/golie/shim"

	"github.com/meop/ghpm/internal/store"
)

// Stamper is ghpm's: shims in bin, kebab under vendor, and a shim that points
// into the extract dir is one ghpm created — anything else in the bin
// directory belongs to somebody else.
func Stamper() (golie.Stamper, error) {
	binDir, err := store.BinDir()
	if err != nil {
		return golie.Stamper{}, err
	}
	shimDir, err := store.ShimDir()
	if err != nil {
		return golie.Stamper{}, err
	}
	extracts, err := store.ExtractsDir()
	if err != nil {
		return golie.Stamper{}, err
	}
	return golie.Stamper{Tool: "ghpm", BinDir: binDir, KebabDir: shimDir, Owned: extracts}, nil
}

// IsShim reports whether path is a shim ghpm stamped. It is the guard for every
// write and delete: the bin directory may be shared with binaries ghpm knows
// nothing about, and those are not ghpm's to replace or remove.
func IsShim(path string) bool {
	s, err := Stamper()
	return err == nil && s.IsShim(path)
}

// Create stamps a sheesh shim at ~/.ghpm/bin/shimName that execs the resolved
// binary inside pkgDir/binSubdir when run. kebab selects the appropriate sheesh
// template (console vs GUI on Windows) automatically.
func Create(shimName, binaryName, pkgDir, binSubdir string, force bool) error {
	s, err := Stamper()
	if err != nil {
		return err
	}
	return s.Create(shimName, filepath.Join(pkgDir, binSubdir, binaryName), force)
}

// Remove deletes the shim for shimName from ~/.ghpm/bin/. A file of that name
// that ghpm did not stamp is left alone: ghpm removing its own package should
// never take somebody else's binary with it.
func Remove(shimName string) error {
	s, err := Stamper()
	if err != nil {
		return err
	}
	return s.Remove(shimName)
}
