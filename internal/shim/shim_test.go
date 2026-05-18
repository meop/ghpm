package shim

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func withHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	return tmp
}

func TestCreate_CreatesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only")
	}
	home := withHome(t)
	pkgDir := t.TempDir()
	target := filepath.Join(pkgDir, "fzf")
	if err := os.WriteFile(target, []byte{}, 0755); err != nil {
		t.Fatal(err)
	}
	if err := Create("fzf", "fzf", pkgDir, ""); err != nil {
		t.Fatal(err)
	}
	dest, err := os.Readlink(filepath.Join(home, ".ghpm", "bin", "fzf"))
	if err != nil {
		t.Fatalf("shim not a symlink: %v", err)
	}
	if dest != target {
		t.Errorf("symlink → %q, want %q", dest, target)
	}
}

func TestCreate_ReplacesExisting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only")
	}
	home := withHome(t)
	pkgDir := t.TempDir()

	v1 := filepath.Join(pkgDir, "v1", "fzf")
	if err := os.MkdirAll(filepath.Dir(v1), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(v1, []byte{}, 0755); err != nil {
		t.Fatal(err)
	}
	if err := Create("fzf", "fzf", filepath.Join(pkgDir, "v1"), ""); err != nil {
		t.Fatal(err)
	}

	v2 := filepath.Join(pkgDir, "v2", "fzf")
	if err := os.MkdirAll(filepath.Dir(v2), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(v2, []byte{}, 0755); err != nil {
		t.Fatal(err)
	}
	if err := Create("fzf", "fzf", filepath.Join(pkgDir, "v2"), ""); err != nil {
		t.Fatal(err)
	}

	dest, err := os.Readlink(filepath.Join(home, ".ghpm", "bin", "fzf"))
	if err != nil {
		t.Fatal(err)
	}
	if dest != v2 {
		t.Errorf("symlink → %q, want %q", dest, v2)
	}
}

func TestRemove_RemovesShim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only")
	}
	home := withHome(t)
	pkgDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(pkgDir, "fzf"), []byte{}, 0755); err != nil {
		t.Fatal(err)
	}
	if err := Create("fzf", "fzf", pkgDir, ""); err != nil {
		t.Fatal(err)
	}
	if err := Remove("fzf"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".ghpm", "bin", "fzf")); !os.IsNotExist(err) {
		t.Error("shim still exists after Remove")
	}
}

func TestRemove_Idempotent(t *testing.T) {
	withHome(t)
	if err := Remove("doesnotexist"); err != nil {
		t.Errorf("Remove of nonexistent shim returned error: %v", err)
	}
}
