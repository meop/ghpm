package cli

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/meop/ghpm/internal/store"
)

func userFontDir() (string, error) {
	home, err := store.UserHomeDir()
	if err != nil {
		return "", err
	}

	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Fonts"), nil
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" || store.UsingTestHome() {
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(localAppData, "Microsoft", "Windows", "Fonts"), nil
	default:
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "fonts"), nil
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

// staleFontPaths returns the old font paths whose file name is not among the
// newly installed paths. Fonts live on disk keyed by base name, so an old path
// sharing a base name with a new one must be kept — uninstalling it would
// delete the file just written for the new version.
func staleFontPaths(oldFonts map[string]string, newPaths []string) []string {
	keep := make(map[string]bool, len(newPaths))
	for _, p := range newPaths {
		keep[filepath.Base(p)] = true
	}
	var stale []string
	for _, p := range oldFonts {
		if !keep[filepath.Base(p)] {
			stale = append(stale, p)
		}
	}
	return stale
}

func uninstallFont(fontKey, fontsDir string) error {
	fontName := filepath.Base(fontKey)
	err := os.Remove(filepath.Join(fontsDir, fontName))
	if os.IsNotExist(err) {
		err = nil
	}
	unregisterFont(fontName)
	return err
}

func fontInstalled(fontKey, fontsDir string) bool {
	if _, err := os.Lstat(filepath.Join(fontsDir, filepath.Base(fontKey))); os.IsNotExist(err) {
		return false
	}
	return fontRegistered(filepath.Base(fontKey))
}
