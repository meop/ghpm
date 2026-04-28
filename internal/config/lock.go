package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

var lockPathFn = defaultLockPath

func defaultLockPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ghpm", ".lock"), nil
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

	for i := 0; i < 3; i++ {
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
