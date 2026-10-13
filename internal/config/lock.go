package config

import (
	"path/filepath"

	"github.com/meop/golie/lock"

	"github.com/meop/ghpm/internal/store"
)

var lockPathFn = defaultLockPath

func defaultLockPath() (string, error) {
	dir, err := store.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".lock"), nil
}

// CheckLock attempts a single non-blocking lock acquisition to detect whether
// another ghpm process is running. Returns nil if the lock is free.
func CheckLock() error {
	path, err := lockPathFn()
	if err != nil {
		return err
	}
	return lock.Check(path, "ghpm")
}

func AcquireLock() (func(), error) {
	path, err := lockPathFn()
	if err != nil {
		return nil, err
	}
	return lock.Acquire(path, "ghpm")
}
