package ghbin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("GHPM_TEST_HOME", tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	return tmp
}

func TestFind_ResolvesTheVendoredCopy(t *testing.T) {
	withHome(t)
	vendored, err := VendorPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(vendored), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vendored, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	got, err := Find()
	if err != nil {
		t.Fatal(err)
	}
	if got != vendored {
		t.Errorf("want the vendored gh at %s, got %s", vendored, got)
	}
}

// TestFind_NeverFallsBackToPath is the other half of Ensure always vendoring
// regardless of PATH: Find must not quietly hand back a PATH gh either, or
// ghpm would still end up running an unvendored, unisolated copy.
func TestFind_NeverFallsBackToPath(t *testing.T) {
	withHome(t)
	fakePathGH(t)

	if _, err := Find(); err == nil {
		t.Fatal("expected Find to fail rather than fall back to a PATH gh")
	}
}

func TestVendorPath_IsNotOnPath(t *testing.T) {
	home := withHome(t)
	vendored, err := VendorPath()
	if err != nil {
		t.Fatal(err)
	}
	// vendor sits beside bin, never inside it: bin is what the user puts on PATH
	binDir := filepath.Join(home, ".ghpm", "bin")
	if strings.HasPrefix(vendored, binDir+string(filepath.Separator)) {
		t.Errorf("the vendored gh must not live under %s, got %s", binDir, vendored)
	}
}

func TestCommand_GivesTheVendoredCopyItsOwnConfigDir(t *testing.T) {
	withHome(t)
	vendored, err := VendorPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(vendored), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vendored, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	cfgDir, err := ConfigDir()
	if err != nil {
		t.Fatal(err)
	}

	cmd, err := Command("auth", "status")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, kv := range cmd.Env {
		if kv == "GH_CONFIG_DIR="+cfgDir {
			found = true
		}
	}
	if !found {
		t.Errorf("the vendored gh should run against %s, env was %v", cfgDir, cmd.Env)
	}
}
