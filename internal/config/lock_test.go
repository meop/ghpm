package config

import (
	"path/filepath"
	"testing"
)

func TestAcquireAndReleaseLock(t *testing.T) {
	dir := t.TempDir()
	orig := lockPathFn
	lockPathFn = func() (string, error) { return filepath.Join(dir, ".lock"), nil }
	defer func() { lockPathFn = orig }()

	unlock, err := AcquireLock()
	if err != nil {
		t.Fatalf("AcquireLock() error: %v", err)
	}
	unlock()
}

func TestLockContention(t *testing.T) {
	dir := t.TempDir()
	orig := lockPathFn
	lockPathFn = func() (string, error) { return filepath.Join(dir, ".lock"), nil }
	defer func() { lockPathFn = orig }()

	unlock1, err := AcquireLock()
	if err != nil {
		t.Fatalf("first AcquireLock() error: %v", err)
	}

	_, err = AcquireLock()
	if err == nil {
		t.Error("expected error when lock is already held")
		unlock1()
		return
	}
	unlock1()
}

func TestCheckLock_NoLockFile(t *testing.T) {
	dir := t.TempDir()
	orig := lockPathFn
	lockPathFn = func() (string, error) { return filepath.Join(dir, ".lock"), nil }
	defer func() { lockPathFn = orig }()

	if err := CheckLock(); err != nil {
		t.Errorf("expected nil for a nonexistent lock file, got %v", err)
	}
}

func TestCheckLock_FreeLock(t *testing.T) {
	dir := t.TempDir()
	orig := lockPathFn
	lockPathFn = func() (string, error) { return filepath.Join(dir, ".lock"), nil }
	defer func() { lockPathFn = orig }()

	unlock, err := AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	unlock() // lock file now exists on disk but isn't held

	if err := CheckLock(); err != nil {
		t.Errorf("expected nil for an existing but unheld lock file, got %v", err)
	}
}

func TestCheckLock_HeldLock(t *testing.T) {
	dir := t.TempDir()
	orig := lockPathFn
	lockPathFn = func() (string, error) { return filepath.Join(dir, ".lock"), nil }
	defer func() { lockPathFn = orig }()

	unlock, err := AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	if err := CheckLock(); err == nil {
		t.Error("expected an error when the lock is held")
	}
}

func TestLockReuse(t *testing.T) {
	dir := t.TempDir()
	orig := lockPathFn
	lockPathFn = func() (string, error) { return filepath.Join(dir, ".lock"), nil }
	defer func() { lockPathFn = orig }()

	unlock1, err := AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	unlock1()

	unlock2, err := AcquireLock()
	if err != nil {
		t.Fatalf("second AcquireLock() after release error: %v", err)
	}
	unlock2()
}
