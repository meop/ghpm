package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	return tmp
}

func TestBinDir(t *testing.T) {
	home := withHome(t)
	dir, err := BinDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".ghpm", "bin")
	if dir != want {
		t.Errorf("BinDir() = %q, want %q", dir, want)
	}
}

func TestReleaseBaseDir(t *testing.T) {
	home := withHome(t)
	dir, err := ReleaseBaseDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".ghpm", "release")
	if dir != want {
		t.Errorf("ReleaseBaseDir() = %q, want %q", dir, want)
	}
}

func TestReleaseDir_CreatesDir(t *testing.T) {
	withHome(t)
	dir, err := ReleaseDir("github.com/junegunn/fzf", "v0.56.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dir, "junegunn") || !strings.Contains(dir, "fzf") || !strings.Contains(dir, "v0.56.0") {
		t.Errorf("unexpected path: %s", dir)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("ReleaseDir did not create directory: %v", err)
	}
}

func TestReleaseDir_Structure(t *testing.T) {
	home := withHome(t)
	dir, err := ReleaseDir("github.com/cli/cli", "v2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	// Expect: ~/.ghpm/release/github.com/cli/cli/v2.0.0
	want := filepath.Join(home, ".ghpm", "release", "github.com", "cli", "cli", "v2.0.0")
	if dir != want {
		t.Errorf("ReleaseDir = %q, want %q", dir, want)
	}
}
