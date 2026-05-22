package shim

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func withHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	return tmp
}

// fakeKebab writes a script (or .bat on Windows) to shimDir that records its
// args to a file alongside it, then exits 0.
func fakeKebab(t *testing.T, shimDir string) {
	t.Helper()
	if err := os.MkdirAll(shimDir, 0755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		script := "@echo off\necho %* > \"%~dp0kebab-args.txt\"\n"
		if err := os.WriteFile(filepath.Join(shimDir, "kebab.bat"), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	} else {
		script := "#!/bin/sh\necho \"$@\" > \"$(dirname \"$0\")/kebab-args.txt\"\n"
		if err := os.WriteFile(filepath.Join(shimDir, "kebab"), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCreate_InvokesKebabWithPaths(t *testing.T) {
	home := withHome(t)
	shimDir := filepath.Join(home, ".ghpm", "shim")
	fakeKebab(t, shimDir)

	pkgDir := t.TempDir()
	srcBin := filepath.Join(pkgDir, exeName("fzf"))
	if err := os.WriteFile(srcBin, []byte{}, 0755); err != nil {
		t.Fatal(err)
	}

	if err := Create("fzf", "fzf", pkgDir, ""); err != nil {
		t.Fatal(err)
	}

	argsFile := filepath.Join(shimDir, "kebab-args.txt")
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("kebab-args.txt not written: %v", err)
	}
	got := strings.TrimSpace(string(data))

	wantSource := srcBin
	wantTarget := filepath.Join(home, ".ghpm", "bin", exeName("fzf"))
	if !strings.Contains(got, wantSource) {
		t.Errorf("args %q missing source path %q", got, wantSource)
	}
	if !strings.Contains(got, wantTarget) {
		t.Errorf("args %q missing target path %q", got, wantTarget)
	}
}

func TestCreate_KebabMissing(t *testing.T) {
	withHome(t)
	pkgDir := t.TempDir()
	err := Create("fzf", "fzf", pkgDir, "")
	if err == nil {
		t.Fatal("expected error when kebab is absent")
	}
	if !strings.Contains(err.Error(), "kebab not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRemove_RemovesShim(t *testing.T) {
	home := withHome(t)
	binDir := filepath.Join(home, ".ghpm", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	shimPath := filepath.Join(binDir, exeName("fzf"))
	if err := os.WriteFile(shimPath, []byte{}, 0755); err != nil {
		t.Fatal(err)
	}

	if err := Remove("fzf"); err != nil {
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
