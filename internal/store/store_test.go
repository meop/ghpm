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
	t.Setenv("GHPM_TEST_HOME", tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("LOCALAPPDATA", filepath.Join(tmp, "AppData", "Local"))
	return tmp
}

func TestUserHomeDir_RefusesUnisolatedTestProcess(t *testing.T) {
	t.Setenv("GHPM_TEST_HOME", "")
	if _, err := UserHomeDir(); err == nil {
		t.Fatal("expected test process without GHPM_TEST_HOME to be rejected")
	}
}

func TestUsingTestHome(t *testing.T) {
	t.Setenv("GHPM_TEST_HOME", "")
	if UsingTestHome() {
		t.Error("expected false with GHPM_TEST_HOME unset")
	}
	withHome(t)
	if !UsingTestHome() {
		t.Error("expected true with GHPM_TEST_HOME set")
	}
}

func TestExtractsDir(t *testing.T) {
	home := withHome(t)
	dir, err := ExtractsDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".ghpm", "extract")
	if dir != want {
		t.Errorf("ExtractsDir() = %q, want %q", dir, want)
	}
}

func TestAKeysDirHoldsItsVersionsItsStageAndItsLatestLink(t *testing.T) {
	home := withHome(t)
	key := filepath.Join(home, ".ghpm", "extract", "fzf@14")
	for name, got := range map[string]func() (string, error){
		key:                               func() (string, error) { return ExtractDir("fzf@14") },
		filepath.Join(key, "latest"):      func() (string, error) { return LatestDir("fzf@14") },
		filepath.Join(key, "14.1.0"):      func() (string, error) { return VersionDir("fzf@14", "14.1.0") },
		filepath.Join(key, ".14.1.0.new"): func() (string, error) { return StageDir("fzf@14", "14.1.0") },
	} {
		if dir, err := got(); err != nil || dir != name {
			t.Errorf("got %q, %v; want %q", dir, err, name)
		}
	}
}

func TestReleaseBaseDir(t *testing.T) {
	home := withHome(t)
	dir, err := ReleaseBaseDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".ghpm", "download")
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
	if !strings.Contains(dir, "junegunn") || !strings.Contains(dir, "fzf") || !strings.Contains(dir, "0.56.0") {
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
	want := filepath.Join(home, ".ghpm", "download", "github.com", "cli", "cli", "2.0.0")
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
	want := filepath.Join(home, ".ghpm", "repo")
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
	want := filepath.Join(home, ".ghpm", "repo", "github.com", "meop", "ghpm-config")
	if dir != want {
		t.Errorf("RepoDir = %q, want %q", dir, want)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("RepoDir did not create directory: %v", err)
	}
}
