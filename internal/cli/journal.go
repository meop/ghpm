package cli

import (
	"os"
	"path/filepath"

	gjournal "github.com/meop/golie/journal"

	"github.com/meop/ghpm/internal/shim"
)

// journal is golie's: the manifest is the commit point of every mutating op,
// and a package that fails short of it is rolled back to what the manifest
// still describes. See golie's journal package for the ordering rules.
type journal = gjournal.Journal

// swapIn puts the install staged at stage in live's place. See golie's journal.Swap.
func swapIn(stage, live string) (aside string, undo func() error, err error) {
	return gjournal.Swap(stage, live)
}

// oldRoot is where the install a swap moved aside keeps its files. One made
// before extract dirs lost their version keeps them in a dir of that version.
func oldRoot(aside, oldVersion string) string {
	if aside == "" || oldVersion == "" {
		return aside
	}
	if fi, err := os.Stat(filepath.Join(aside, oldVersion)); err == nil && fi.IsDir() {
		return filepath.Join(aside, oldVersion)
	}
	return aside
}

// shimUndo reverses a shim ghpm is about to stamp or delete. See golie's journal.ShimUndo.
func shimUndo(shimName string) func() error {
	s, err := shim.Stamper()
	if err != nil {
		return func() error { return err }
	}
	return gjournal.ShimUndo(s, shimName)
}

// fontUndo mirrors shimUndo for an installed font. Fonts live in the user font
// dir keyed by base name, so replacing one whose name is unchanged is an
// overwrite — reinstalling the old copy is the whole undo — while a renamed
// one leaves two files and the new one has to go before the old comes back.
func fontUndo(newFontPath, oldFontPath, oldPkgDir, fontsDir string) func() error {
	return func() error {
		if newFontPath != "" && filepath.Base(newFontPath) != filepath.Base(oldFontPath) {
			if err := uninstallFont(newFontPath, fontsDir); err != nil {
				return err
			}
		}
		if oldFontPath == "" {
			return nil
		}
		return installFont(filepath.Join(oldPkgDir, filepath.FromSlash(oldFontPath)), fontsDir)
	}
}
