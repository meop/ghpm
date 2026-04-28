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

func TestPackagesDir(t *testing.T) {
	home := withHome(t)
	dir, err := PackagesDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".ghpm", "packages")
	if dir != want {
		t.Errorf("PackagesDir() = %q, want %q", dir, want)
	}
}

func TestPackageDir_CreatesDir(t *testing.T) {
	withHome(t)
	dir, err := PackageDir("fzf")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dir, "fzf") {
		t.Errorf("unexpected path: %s", dir)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("PackageDir did not create directory: %v", err)
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

func TestReposBaseDir(t *testing.T) {
	home := withHome(t)
	dir, err := ReposBaseDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".ghpm", "repos")
	if dir != want {
		t.Errorf("ReposBaseDir() = %q, want %q", dir, want)
	}
}

func TestRepoDir_Structure(t *testing.T) {
	home := withHome(t)
	dir, err := RepoDir("github.com/meop/ghpm-config")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".ghpm", "repos", "github.com", "meop", "ghpm-config")
	if dir != want {
		t.Errorf("RepoDir = %q, want %q", dir, want)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("RepoDir did not create directory: %v", err)
	}
}
