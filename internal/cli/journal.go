package cli

import (
	"os"
	"path/filepath"

	gjournal "github.com/meop/golie/journal"
	"github.com/meop/golie/layout"
	"github.com/meop/golie/pointer"
	"github.com/meop/golie/tidy"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/shim"
)

// journal is golie's: the manifest is the commit point of every mutating op,
// and a package that fails short of it is rolled back to what the manifest
// still describes. See golie's journal package for the ordering rules.
type journal = gjournal.Journal

// placeIn puts the version unpacked at stage at dir. See golie's journal.Place.
func placeIn(stage, dir string) (placed string, undo func() error, err error) {
	return gjournal.Place(stage, dir)
}

// pointVersion moves key's version file to dir. See golie's journal.Point.
func pointVersion(file, dir string) (undo func() error, err error) {
	return gjournal.Point(file, dir)
}

// oldRoot is where the install the version file is about to leave keeps its
// files: the file's target, or with no version file the version's dir, or the
// key's dir itself, as ghpm 0.14.0 unpacked.
func oldRoot(keyDir, oldVersion string) string {
	if target, ok := pointer.Target(filepath.Join(keyDir, layout.VersionFile)); ok {
		return target
	}
	if fi, err := os.Stat(filepath.Join(keyDir, oldVersion)); oldVersion != "" && err == nil && fi.IsDir() {
		return filepath.Join(keyDir, oldVersion)
	}
	return keyDir
}

// installVersion places the unpacked version and points the version file at
// it, journalling both, and returns where it placed it.
func installVersion(jrn *journal, ex extractResult, version string) (string, error) {
	placed, undo, err := placeIn(ex.stageDir, ex.versionDir)
	if err != nil {
		return "", err
	}
	jrn.Did("extract "+version, undo)
	undo, err = pointVersion(ex.pkgDir, placed)
	if err != nil {
		return "", err
	}
	jrn.Did("version file "+version, undo)
	return placed, nil
}

// prune clears a key's dir of all but its version file and that version,
// once the manifest no longer needs them, and says what would not go.
func prune(cfg *config.Settings, name, keyDir string) {
	for _, st := range tidy.PruneInstall(keyDir) {
		printWarn(cfg, "%s: could not remove %s, left for tidy: %v", name, filepath.Base(st.Path), st.Err)
	}
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
