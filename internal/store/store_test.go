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
	want := filepath.Join(home, ".ghpm", "releases")
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
	want := filepath.Join(home, ".ghpm", "releases", "github.com", "cli", "cli", "v2.0.0")
	if dir != want {
		t.Errorf("ReleaseDir = %q, want %q", dir, want)
	}
}

func TestToolsBaseDir(t *testing.T) {
	home := withHome(t)
	dir, err := ToolsBaseDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".ghpm", "tools")
	if dir != want {
		t.Errorf("ToolsBaseDir() = %q, want %q", dir, want)
	}
}

func TestToolDir_Structure(t *testing.T) {
	home := withHome(t)
	dir, err := ToolDir("github.com/meop/ghpm-config")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".ghpm", "tools", "github.com", "meop", "ghpm-config")
	if dir != want {
		t.Errorf("ToolDir = %q, want %q", dir, want)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("ToolDir did not create directory: %v", err)
	}
}
