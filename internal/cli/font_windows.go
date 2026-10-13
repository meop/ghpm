//go:build windows

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"

	"github.com/meop/ghpm/internal/store"
)

const fontRegKey = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`

func regFontName(fontFile string) string {
	return strings.TrimSuffix(fontFile, filepath.Ext(fontFile)) + " (TrueType)"
}

// fontRegistry says where a font is registered. The registry is the real
// user's whatever home ghpm was pointed at, so the guard that keeps tests out
// of the real home has to keep them out of HKCU as well: under a test home
// the registry is stood in for by a directory there, and a test with no test
// home gets neither — it must not be able to add to or delete from the fonts
// the person running it has installed.
func fontRegistry() (standIn string, real bool) {
	home, err := store.UserHomeDir()
	if err != nil {
		return "", false
	}
	if store.UsingTestHome() {
		return filepath.Join(home, ".font-registry"), false
	}
	return "", true
}

func registerFont(fontFile, fontPath string) error {
	standIn, real := fontRegistry()
	if !real {
		if standIn == "" {
			return errors.New("refusing to register a font in the real registry during tests")
		}
		if err := os.MkdirAll(standIn, 0755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(standIn, regFontName(fontFile)), []byte(fontPath), 0644)
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, fontRegKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer func() { _ = k.Close() }()
	return k.SetStringValue(regFontName(fontFile), fontPath)
}

func unregisterFont(fontFile string) {
	standIn, real := fontRegistry()
	if !real {
		if standIn != "" {
			_ = os.Remove(filepath.Join(standIn, regFontName(fontFile)))
		}
		return
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, fontRegKey, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer func() { _ = k.Close() }()
	_ = k.DeleteValue(regFontName(fontFile))
}

func fontRegistered(fontFile string) bool {
	standIn, real := fontRegistry()
	if !real {
		if standIn == "" {
			return false
		}
		_, err := os.Stat(filepath.Join(standIn, regFontName(fontFile)))
		return err == nil
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, fontRegKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer func() { _ = k.Close() }()
	_, _, err = k.GetStringValue(regFontName(fontFile))
	return err == nil
}
