package cli

import (
	"path/filepath"

	gjournal "github.com/meop/golie/journal"

	"github.com/meop/ghpm/internal/shim"
)

// journal is golie's: the manifest is the commit point of every mutating op,
// and a package that fails short of it is rolled back to what the manifest
// still describes. See golie's journal package for the ordering rules.
type journal = gjournal.Journal

// extractUndo reverses an overlay extraction. A sync --reinstall that lands on the
// version already installed re-extracts in place, so there is nothing to undo
// — and removing that dir would delete the live install.
func extractUndo(newPkgDir, oldVersion, newVersion string) func() error {
	return gjournal.ExtractUndo(newPkgDir, oldVersion, newVersion)
}

// shimUndo reverses a shim ghpm is about to stamp or delete: re-stamping it
// against the extract dir it pointed at before the op, or removing it when the
// op is what created it. An empty oldBinKey means there was no such shim.
func shimUndo(shimName, oldBinKey, oldPkgDir string) func() error {
	s, err := shim.Stamper()
	if err != nil {
		return func() error { return err }
	}
	oldSource := ""
	if oldBinKey != "" {
		binDir, binName := parseBinPath(oldBinKey)
		oldSource = filepath.Join(oldPkgDir, binDir, binName)
	}
	return gjournal.ShimUndo(s, shimName, oldSource)
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
