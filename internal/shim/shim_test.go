package shim

import (
	"encoding/binary"
	golie "github.com/meop/golie/shim"

	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/faketool"
	"github.com/meop/ghpm/internal/store"
)

func stampedBody(target string) []byte {
	n := make([]byte, 4)
	binary.LittleEndian.PutUint32(n, uint32(len(target)))
	return append(append([]byte("a wedge template"+target), n...), "SHIM"...)
}

func withHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("GHPM_TEST_HOME", tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("LOCALAPPDATA", filepath.Join(tmp, "AppData", "Local"))
	return tmp
}

// fakeMallet stands a script in for mallet in shimDir that records its args to
// a file alongside it, then exits 0.
func fakeMallet(t *testing.T, shimDir string) {
	t.Helper()
	faketool.Write(t, filepath.Join(shimDir, golie.ExeName("mallet")), `echo "$@" > "$(dirname "$0")/mallet-args.txt"`)
}

func TestCreate_InvokesMalletWithPaths(t *testing.T) {
	home := withHome(t)
	shimDir, err := store.ShimDir()
	if err != nil {
		t.Fatal(err)
	}
	fakeMallet(t, shimDir)

	pkgDir := t.TempDir()
	srcBin := filepath.Join(pkgDir, golie.ExeName("fzf"))
	if err := os.WriteFile(srcBin, []byte{}, 0755); err != nil {
		t.Fatal(err)
	}

	if err := Create(golie.ExeName("fzf"), golie.ExeName("fzf"), pkgDir, "", false); err != nil {
		t.Fatal(err)
	}

	argsFile := filepath.Join(shimDir, "mallet-args.txt")
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("mallet-args.txt not written: %v", err)
	}
	got := strings.TrimSpace(string(data))

	wantSource := srcBin
	wantTarget := filepath.Join(home, ".ghpm", "bin", golie.ExeName("fzf"))
	if !strings.Contains(got, wantSource) {
		t.Errorf("args %q missing source path %q", got, wantSource)
	}
	if !strings.Contains(got, wantTarget) {
		t.Errorf("args %q missing target path %q", got, wantTarget)
	}
}

func TestCreate_MalletMissing(t *testing.T) {
	withHome(t)
	pkgDir := t.TempDir()
	err := Create(golie.ExeName("fzf"), golie.ExeName("fzf"), pkgDir, "", false)
	if err == nil {
		t.Fatal("expected error when mallet is absent")
	}
	if !strings.Contains(err.Error(), "mallet not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRemove_RemovesShim(t *testing.T) {
	home := withHome(t)
	binDir := filepath.Join(home, ".ghpm", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	shimPath := filepath.Join(binDir, golie.ExeName("fzf"))
	writeFakeShim(t, shimPath)

	if err := Remove(golie.ExeName("fzf")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(shimPath); !os.IsNotExist(err) {
		t.Error("shim still exists after Remove")
	}
}

func TestRemove_Idempotent(t *testing.T) {
	withHome(t)
	if err := Remove("doesnotexist"); err != nil {
		t.Errorf("Remove of nonexistent shim returned error: %v", err)
	}
}

// writeFakeShim stamps what IsShim looks for: a source path inside ghpm's
// extract dir.
func writeFakeShim(t *testing.T, path string) {
	t.Helper()
	extracts, err := store.ExtractsDir()
	if err != nil {
		t.Fatal(err)
	}
	body := stampedBody(filepath.Join(extracts, "fzf", "1.0", "fzf"))
	if err := os.WriteFile(path, body, 0755); err != nil {
		t.Fatal(err)
	}
}

func TestIsShim_OnlyOurOwn(t *testing.T) {
	home := withHome(t)
	binDir := filepath.Join(home, ".ghpm", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}

	ours := filepath.Join(binDir, "fzf")
	writeFakeShim(t, ours)
	if !IsShim(ours) {
		t.Error("a shim ghpm stamped should be recognized")
	}

	theirs := filepath.Join(binDir, "someone-elses")
	if err := os.WriteFile(theirs, []byte("#!/bin/sh\necho hi\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if IsShim(theirs) {
		t.Error("a binary ghpm did not stamp must not be claimed")
	}

	// a wedge shim pointing somewhere else is not ghpm's to touch either
	elsewhere := filepath.Join(binDir, "elsewhere")
	if err := os.WriteFile(elsewhere, stampedBody("/opt/other/bin/tool"), 0755); err != nil {
		t.Fatal(err)
	}
	if IsShim(elsewhere) {
		t.Error("a shim pointing outside ghpm's extract dir must not be claimed")
	}
}

func TestRemove_LeavesForeignBinaryAlone(t *testing.T) {
	home := withHome(t)
	binDir := filepath.Join(home, ".ghpm", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	theirs := filepath.Join(binDir, golie.ExeName("fzf"))
	if err := os.WriteFile(theirs, []byte("not a shim"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := Remove(golie.ExeName("fzf")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(theirs); err != nil {
		t.Error("removing a package took a binary ghpm never installed")
	}
}

func TestCreate_RefusesToReplaceForeignBinaryWithoutForce(t *testing.T) {
	home := withHome(t)
	binDir := filepath.Join(home, ".ghpm", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	theirs := filepath.Join(binDir, golie.ExeName("fzf"))
	if err := os.WriteFile(theirs, []byte("not a shim"), 0755); err != nil {
		t.Fatal(err)
	}

	err := Create(golie.ExeName("fzf"), golie.ExeName("fzf"), t.TempDir(), "", false)
	if err == nil {
		t.Fatal("expected a refusal to overwrite a foreign binary")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("the refusal should say how to override it, got %v", err)
	}
	body, _ := os.ReadFile(theirs)
	if string(body) != "not a shim" {
		t.Error("the foreign binary was modified despite the refusal")
	}
}
