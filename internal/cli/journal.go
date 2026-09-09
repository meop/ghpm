package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/meop/ghpm/internal/shim"
)

// journal records the filesystem mutations one package's op has already
// applied, each paired with the undo that reverses it.
//
// The manifest is the commit point: an entry is written only once a package's
// whole op succeeded, and anything short of that rolls the journal back, so
// ~/.ghpm is left exactly as the op found it and the untouched manifest still
// describes it. The alternative — writing what happened to survive — is what
// corrupts state: a sync whose shim create failed on a locked binary (the
// running shell's own pwsh.exe) used to still record the new version and drop
// the shim it could not re-point, leaving a manifest that claimed a version
// that was not on disk and had forgotten a shim that was.
//
// This is also why the old extract dir is removed only *after* the manifest
// write: every undo below restores from it.
type journal struct {
	steps []journalStep
}

type journalStep struct {
	what string
	undo func() error
}

// did records a mutation that has already happened. A nil undo is dropped:
// some mutations (re-extracting over the dir already installed) genuinely have
// nothing to reverse, and the call sites read better saying so than branching.
func (j *journal) did(what string, undo func() error) {
	if undo == nil {
		return
	}
	j.steps = append(j.steps, journalStep{what: what, undo: undo})
}

// rollback reverses every recorded step, newest first, and returns one error
// per step it could not undo. It never stops early: the step that resists
// undoing is typically the very thing that failed the op (a locked file), and
// letting it strand the steps recorded before it would leave exactly the
// half-applied state rolling back exists to prevent.
func (j *journal) rollback() []error {
	var errs []error
	for _, step := range slices.Backward(j.steps) {
		if err := step.undo(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", step.what, err))
		}
	}
	j.steps = nil
	return errs
}

// extractUndo reverses an overlay extraction. A force sync that lands on the
// version already installed re-extracts in place, so there is nothing to undo
// — and removing that dir would delete the live install.
func extractUndo(newPkgDir, oldVersion, newVersion string) func() error {
	if oldVersion == newVersion {
		return nil
	}
	return func() error { return os.RemoveAll(newPkgDir) }
}

// shimUndo reverses a shim ghpm is about to stamp or delete: re-stamping it
// against the extract dir it pointed at before the op, or removing it when the
// op is what created it. An empty oldBinKey means there was no such shim.
func shimUndo(shimName, oldBinKey, oldPkgDir string) func() error {
	if oldBinKey == "" {
		return func() error { return shim.Remove(shimName) }
	}
	binDir, binName := parseBinPath(oldBinKey)
	return func() error { return shim.Create(shimName, binName, oldPkgDir, binDir, true) }
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
