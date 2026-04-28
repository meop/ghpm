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
