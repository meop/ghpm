package asset

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeExec(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
}

func TestFindAndMoveBinary_Hint(t *testing.T) {
	tmp, dest := t.TempDir(), t.TempDir()
	writeExec(t, tmp, "rg")
	writeExec(t, tmp, "ripgrep")

	archiveName, _, err := findAndMoveBinary(tmp, dest, "ripgrep", "rg")
	if err != nil {
		t.Fatal(err)
	}
	if archiveName != "rg" {
		t.Errorf("archiveName = %q, want %q", archiveName, "rg")
	}
	if _, err := os.Stat(filepath.Join(dest, "ripgrep")); err != nil {
		t.Errorf("output not at dest/ripgrep: %v", err)
	}
}

func TestFindAndMoveBinary_NameMatch(t *testing.T) {
	tmp, dest := t.TempDir(), t.TempDir()
	writeExec(t, tmp, "rg")
	writeExec(t, tmp, "ripgrep")

	archiveName, _, err := findAndMoveBinary(tmp, dest, "ripgrep", "")
	if err != nil {
		t.Fatal(err)
	}
	if archiveName != "ripgrep" {
		t.Errorf("archiveName = %q, want %q", archiveName, "ripgrep")
	}
}

func TestFindAndMoveBinary_SingleCandidate(t *testing.T) {
	tmp, dest := t.TempDir(), t.TempDir()
	writeExec(t, tmp, "weirdname")

	archiveName, _, err := findAndMoveBinary(tmp, dest, "mytool", "")
	if err != nil {
		t.Fatal(err)
	}
	if archiveName != "weirdname" {
		t.Errorf("archiveName = %q, want %q", archiveName, "weirdname")
	}
	if _, err := os.Stat(filepath.Join(dest, "mytool")); err != nil {
		t.Errorf("output not at dest/mytool: %v", err)
	}
}

func TestFindAndMoveBinary_NonExecutableFallback(t *testing.T) {
	tmp, dest := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "mytool"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := findAndMoveBinary(tmp, dest, "mytool", ""); err != nil {
		t.Errorf("expected fallback to non-executable to succeed: %v", err)
	}
}

func TestFindAndMoveBinary_NoFiles(t *testing.T) {
	tmp, dest := t.TempDir(), t.TempDir()
	if _, _, err := findAndMoveBinary(tmp, dest, "mytool", ""); err == nil {
		t.Error("expected error for empty dir")
	}
}

func TestFindAndMoveBinary_Skip(t *testing.T) {
	tmp, dest := t.TempDir(), t.TempDir()
	writeExec(t, tmp, "foo")
	writeExec(t, tmp, "bar")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	if _, err = w.WriteString("0\n"); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}

	_, _, moveErr := findAndMoveBinary(tmp, dest, "mytool", "")
	if !errors.Is(moveErr, ErrSkip) {
		t.Errorf("expected ErrSkip, got %v", moveErr)
	}
}
