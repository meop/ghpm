package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"

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
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	fl := flock.New(path)
	ok, err := fl.TryLock()
	if err != nil {
		return fmt.Errorf("checking lock: %w", err)
	}
	if !ok {
		return fmt.Errorf("lock held — another ghpm process may be running")
	}
	_ = fl.Unlock()
	return nil
}

func AcquireLock() (func(), error) {
	path, err := lockPathFn()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	fl := flock.New(path)

	for i := range 3 {
		ok, err := fl.TryLock()
		if err != nil {
			return nil, fmt.Errorf("acquiring lock: %w", err)
		}
		if ok {
			return func() { _ = fl.Unlock() }, nil
		}
		if i < 2 {
			time.Sleep(time.Second)
		}
	}

	return nil, fmt.Errorf("another ghpm process is running (lock held on %s)", path)
}
