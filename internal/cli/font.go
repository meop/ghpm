package cli

import (
	"os"
	"path/filepath"
	"runtime"
)

func userFontDir() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Fonts"), nil
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(localAppData, "Microsoft", "Windows", "Fonts"), nil
	default:
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "fonts"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "fonts"), nil
	}
}

func ensureFontDir() (string, error) {
	fontsDir, err := userFontDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(fontsDir, 0755); err != nil {
		return "", err
	}
	return fontsDir, nil
}

func installFont(srcPath, fontsDir string) error {
	dst := filepath.Join(fontsDir, filepath.Base(srcPath))
	_ = os.Remove(dst) // fontdrvhost opens fonts with FILE_SHARE_DELETE so Remove succeeds even on in-use files, freeing the path for the subsequent write
	if err := copyFile(srcPath, dst); err != nil {
		return err
	}
	return registerFont(filepath.Base(srcPath), dst)
}

func uninstallFont(fontKey, fontsDir string) {
	fontName := filepath.Base(fontKey)
	_ = os.Remove(filepath.Join(fontsDir, fontName))
	unregisterFont(fontName)
}

func fontInstalled(fontKey, fontsDir string) bool {
	if _, err := os.Lstat(filepath.Join(fontsDir, filepath.Base(fontKey))); os.IsNotExist(err) {
		return false
	}
	return fontRegistered(filepath.Base(fontKey))
}
