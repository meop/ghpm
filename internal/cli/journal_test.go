package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/store"
)

func TestJournal_RollbackReversesNewestFirst(t *testing.T) {
	var order []string
	jrn := &journal{}
	jrn.Did("first", func() error { order = append(order, "first"); return nil })
	jrn.Did("second", func() error { order = append(order, "second"); return nil })
	jrn.Did("third", func() error { order = append(order, "third"); return nil })

	if errs := jrn.Rollback(); len(errs) != 0 {
		t.Fatalf("unexpected rollback errors: %v", errs)
	}
	if strings.Join(order, ",") != "third,second,first" {
		t.Errorf("expected newest-first undo order, got %v", order)
	}
	if errs := jrn.Rollback(); len(errs) != 0 || len(order) != 3 {
		t.Errorf("expected a rolled-back journal to be spent, got %v after %v", order, errs)
	}
}

// TestJournal_RollbackContinuesPastAFailingUndo covers the case rollback
// exists for: the step that resists undoing is usually the locked file that
// failed the op in the first place, and stopping there would strand every
// earlier step half-applied — exactly the state being rolled back from.
func TestJournal_RollbackContinuesPastAFailingUndo(t *testing.T) {
	undone := 0
	jrn := &journal{}
	jrn.Did("earlier", func() error { undone++; return nil })
	jrn.Did("locked", func() error { return errors.New("file in use") })

	errs := jrn.Rollback()
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "locked: file in use") {
		t.Fatalf("expected the failing step named in one error, got %v", errs)
	}
	if undone != 1 {
		t.Errorf("expected the earlier step to still be undone, undone = %d", undone)
	}
}

func TestJournal_DidDropsNilUndo(t *testing.T) {
	jrn := &journal{}
	jrn.Did("nothing to reverse", nil)
	if jrn.Len() != 0 {
		t.Errorf("expected a nil undo to record no step, got %d", jrn.Len())
	}
}

func TestOldRoot_FindsTheVersionDirOfAnInstallMadeBeforeTheStableLayout(t *testing.T) {
	aside := t.TempDir()
	if got := oldRoot(aside, "1.0.0"); got != aside {
		t.Errorf("oldRoot of a stable install = %s, want %s", got, aside)
	}
	if err := os.MkdirAll(filepath.Join(aside, "1.0.0", "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := oldRoot(aside, "1.0.0"); got != filepath.Join(aside, "1.0.0") {
		t.Errorf("oldRoot of a versioned install = %s", got)
	}
	if got := oldRoot("", "1.0.0"); got != "" {
		t.Errorf("oldRoot of a first install = %s", got)
	}
}

// TestShimUndo_RestampsAtWhatTheShimRanBefore is the heart of the sync
// rollback: a shim ghpm re-pointed (or destroyed on the way to re-pointing)
// goes back to the target it named when the undo was recorded.
func TestShimUndo_RestampsAtWhatTheShimRanBefore(t *testing.T) {
	withHome(t)
	fakeMallet(t)
	binDir, err := store.BinDir()
	if err != nil {
		t.Fatal(err)
	}
	extracts, err := store.ExtractsDir()
	if err != nil {
		t.Fatal(err)
	}
	before := filepath.Join(extracts, "pwsh", "1.0.0", "pwsh")
	shimPath := filepath.Join(binDir, exeName("pwsh"))
	if err := os.WriteFile(shimPath, stampedBody(before), 0755); err != nil {
		t.Fatal(err)
	}

	undo := shimUndo("pwsh")
	if err := os.WriteFile(shimPath, stampedBody(filepath.Join(extracts, "pwsh", "pwsh")), 0755); err != nil {
		t.Fatal(err)
	}
	if err := undo(); err != nil {
		t.Fatal(err)
	}

	shimDir, err := store.ShimDir()
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(filepath.Join(shimDir, "mallet-args.txt"))
	if err != nil {
		t.Fatalf("expected mallet to have been invoked: %v", err)
	}
	if !strings.Contains(string(args), before) {
		t.Errorf("expected the shim restamped at %s, got %s", before, args)
	}
}

func TestShimUndo_RemovesShimTheOpCreated(t *testing.T) {
	withHome(t)
	binDir, err := store.BinDir()
	if err != nil {
		t.Fatal(err)
	}
	undo := shimUndo("fzf")
	created := filepath.Join(binDir, exeName("fzf"))
	writeFakeShim(t, created)

	if err := undo(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Errorf("expected a shim with no prior version to be removed, stat err = %v", err)
	}
}

// TestFontUndo_ReinstallsOverwrittenFont covers the common shape: the new
// release keeps the font's file name, so installing it overwrote the old copy
// in place and reinstalling from the old extract dir is the whole undo.
func TestFontUndo_ReinstallsOverwrittenFont(t *testing.T) {
	home := withHome(t)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg-data"))
	fontsDir := filepath.Join(home, "xdg-data", "fonts")
	makeFontFile(t, fontsDir, "Hack-Regular.ttf")

	oldPkgDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(oldPkgDir, "Hack-Regular.ttf"), []byte("old font"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := fontUndo("Hack-Regular.ttf", "Hack-Regular.ttf", oldPkgDir, fontsDir)(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(fontsDir, "Hack-Regular.ttf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "old font" {
		t.Errorf("expected the old font restored, got %q", body)
	}
}

// TestFontUndo_RemovesRenamedFontBeforeRestoring covers the other shape: the
// new release renamed the file, so the copy just installed is a second file
// that has to go before the old one comes back.
func TestFontUndo_RemovesRenamedFontBeforeRestoring(t *testing.T) {
	home := withHome(t)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg-data"))
	fontsDir := filepath.Join(home, "xdg-data", "fonts")
	makeFontFile(t, fontsDir, "Hack-Regular.ttf")

	oldPkgDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(oldPkgDir, "Hack-Regular.ttf"), []byte("old font"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := fontUndo("HackNerdFont-Regular.ttf", "Hack-Regular.ttf", oldPkgDir, fontsDir)(); err != nil {
		t.Fatal(err)
	}
	if fontInstalled("HackNerdFont-Regular.ttf", fontsDir) {
		t.Error("expected the renamed font the op installed to be removed")
	}
	if !fontInstalled("Hack-Regular.ttf", fontsDir) {
		t.Error("expected the old font to be restored")
	}
}

// TestSyncBinShims_RollbackRestoresShimAfterFailedCreate is the regression for
// the reported Windows bug: `ghpm up` from inside pwsh found a pwsh update,
// could not restamp ~/.ghpm/bin/pwsh.exe because the running shell held it,
// and the run still advanced the manifest to the new version while dropping
// the shim it had failed to re-point — leaving a manifest that named a version
// not on disk and had forgotten a shim that was, invisible to `ghpm ls` and an
// orphan to `ghpm tidy`. The shim work must journal enough to put the package
// back before the caller decides not to write the entry at all.
func TestSyncBinShims_RollbackRestoresShimAfterFailedCreate(t *testing.T) {
	withHome(t)
	extracts, err := store.ExtractsDir()
	if err != nil {
		t.Fatal(err)
	}
	binDir, err := store.BinDir()
	if err != nil {
		t.Fatal(err)
	}
	oldPkgDir := filepath.Join(extracts, "pwsh", "7.5.0")
	if err := os.WriteFile(filepath.Join(binDir, exeName("pwsh")), stampedBody(filepath.Join(oldPkgDir, "pwsh")), 0755); err != nil {
		t.Fatal(err)
	}

	// No fakeMallet staged yet, so every shim.Create fails the way a locked
	// target does.
	jrn := &journal{}
	installed, errs := syncBinShims(&config.Settings{}, jrn, filepath.Join(extracts, "pwsh"),
		map[string]string{"pwsh": "pwsh"}, map[string]string{"pwsh": "pwsh"}, false)
	if len(errs) == 0 {
		t.Fatal("expected the create to fail with no mallet staged")
	}
	if _, ok := installed["pwsh"]; ok {
		t.Errorf("expected a failed create to not be recorded as installed, got %v", installed)
	}

	// Rolling back has to restamp at what the shim ran before.
	fakeMallet(t)
	if rbErrs := jrn.Rollback(); len(rbErrs) != 0 {
		t.Fatalf("unexpected rollback errors: %v", rbErrs)
	}
	shimDir, err := store.ShimDir()
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(filepath.Join(shimDir, "mallet-args.txt"))
	if err != nil {
		t.Fatalf("expected the shim to be restamped on rollback: %v", err)
	}
	if !strings.Contains(string(args), filepath.Join(oldPkgDir, "pwsh")) {
		t.Errorf("expected restamp against the old extract dir, got %s", args)
	}
}

// TestApplyShimPlan_RollbackRemovesShimsFromAFailedAdd covers add's half: a
// package whose second shim fails must leave nothing of its first behind,
// since no manifest entry will be written to remember it.
func TestApplyShimPlan_RollbackRemovesShimsFromAFailedAdd(t *testing.T) {
	withHome(t)
	binDir, err := store.BinDir()
	if err != nil {
		t.Fatal(err)
	}
	p := shimPlan{
		jobName: "ripgrep",
		pkgDir:  t.TempDir(),
		bin:     map[string]string{"rg": "rg"},
	}
	jrn := &journal{}
	noFontDir := func() (string, error) { return "", nil }
	noFontInstall := func(_, _ string) error { return nil }
	create := func(shimName, _, _, _ string, _ bool) error {
		writeFakeShim(t, filepath.Join(binDir, shimName))
		return nil
	}
	if _, _, failed, _ := applyShimPlan(p, false, jrn, config.PackageEntry{}, "", create, noFontDir, noFontInstall); failed {
		t.Fatal("expected the plan to apply cleanly")
	}
	if _, err := os.Stat(filepath.Join(binDir, "rg")); err != nil {
		t.Fatalf("expected the shim to exist before rollback: %v", err)
	}

	if errs := jrn.Rollback(); len(errs) != 0 {
		t.Fatalf("unexpected rollback errors: %v", errs)
	}
	if _, err := os.Stat(filepath.Join(binDir, "rg")); !os.IsNotExist(err) {
		t.Errorf("expected the shim to be removed on rollback, stat err = %v", err)
	}
}
