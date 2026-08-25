package cli

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/meop/ghpm/internal/ui"
)

// TestApplyShimPlan_FailedShimNotRecorded guards against a failed shim.Create
// (e.g. a stale non-ghpm binary occupying the target) still reading back as installed.
func TestApplyShimPlan_FailedShimNotRecorded(t *testing.T) {
	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	p := shimPlan{
		key:     "gh",
		jobName: "gh",
		pkgDir:  "/fake/pkg",
		bin:     map[string]string{"gh": "bin/gh"},
	}

	failingCreate := func(shimName, binaryName, pkgDir, binSubdir string, force bool) error {
		return fmt.Errorf("%s exists and is not a ghpm shim — pass --force to replace it", shimName)
	}
	unusedEnsureFontDir := func() (string, error) { return "", nil }
	unusedInstallFont := func(srcPath, fontsDir string) error { return nil }

	installedBin, installedFont, failed, failReason := applyShimPlan(p, false, failingCreate, unusedEnsureFontDir, unusedInstallFont)

	if !failed {
		t.Fatal("expected failed=true when shim.Create fails")
	}
	if failReason == "" {
		t.Error("expected a non-empty failReason")
	}
	if len(installedBin) != 0 {
		t.Errorf("expected no installed bins on a failed shim create, got %v", installedBin)
	}
	if len(installedFont) != 0 {
		t.Errorf("expected no installed fonts, got %v", installedFont)
	}
}

func TestApplyShimPlan_PartialSuccessOnlyRecordsWhatSucceeded(t *testing.T) {
	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	p := shimPlan{
		key:     "multi",
		jobName: "multi",
		pkgDir:  "/fake/pkg",
		bin: map[string]string{
			"good": "bin/good",
			"bad":  "bin/bad",
		},
	}

	create := func(shimName, binaryName, pkgDir, binSubdir string, force bool) error {
		if shimName == "bad" {
			return fmt.Errorf("bad exists and is not a ghpm shim")
		}
		return nil
	}
	unusedEnsureFontDir := func() (string, error) { return "", nil }
	unusedInstallFont := func(srcPath, fontsDir string) error { return nil }

	installedBin, _, failed, failReason := applyShimPlan(p, false, create, unusedEnsureFontDir, unusedInstallFont)

	if !failed {
		t.Fatal("expected failed=true when one of two shims fails")
	}
	if failReason == "" {
		t.Error("expected a non-empty failReason")
	}
	if _, ok := installedBin["good"]; !ok {
		t.Error("expected the succeeding shim to be recorded")
	}
	if _, ok := installedBin["bad"]; ok {
		t.Error("expected the failing shim to not be recorded")
	}
	if len(installedBin) != 1 {
		t.Errorf("expected exactly 1 installed bin, got %v", installedBin)
	}
}

func TestApplyShimPlan_AllSucceed(t *testing.T) {
	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	p := shimPlan{
		key:     "fzf",
		jobName: "fzf",
		pkgDir:  "/fake/pkg",
		bin:     map[string]string{"fzf": "bin/fzf"},
		font:    map[string]string{"MyFont": "fonts/MyFont.ttf"},
	}

	create := func(shimName, binaryName, pkgDir, binSubdir string, force bool) error { return nil }
	ensureFontDir := func() (string, error) { return "/fake/fonts", nil }
	installFont := func(srcPath, fontsDir string) error { return nil }

	installedBin, installedFont, failed, failReason := applyShimPlan(p, false, create, ensureFontDir, installFont)

	if failed {
		t.Fatalf("expected failed=false, got failReason=%q", failReason)
	}
	if len(installedBin) != 1 || len(installedFont) != 1 {
		t.Errorf("expected 1 bin and 1 font installed, got bin=%v font=%v", installedBin, installedFont)
	}
}
