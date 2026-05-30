//go:build windows

package cli

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const fontRegKey = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`

func regFontName(fontFile string) string {
	return strings.TrimSuffix(fontFile, filepath.Ext(fontFile)) + " (TrueType)"
}

func registerFont(fontFile, fontPath string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, fontRegKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(regFontName(fontFile), fontPath)
}

func unregisterFont(fontFile string) {
	k, err := registry.OpenKey(registry.CURRENT_USER, fontRegKey, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.DeleteValue(regFontName(fontFile))
}

func fontRegistered(fontFile string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, fontRegKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(regFontName(fontFile))
	return err == nil
}

// findOrphanedFonts returns HKCU font registry entries whose value path is
// inside fontsDir but whose base filename is not in expected.
// expected contains base filenames (e.g. "Hack-Regular.ttf") of all fonts
// currently tracked in the manifest.
func findOrphanedFonts(expected map[string]bool, fontsDir string) []orphanedFontEntry {
	k, err := registry.OpenKey(registry.CURRENT_USER, fontRegKey, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()

	names, err := k.ReadValueNames(-1)
	if err != nil {
		return nil
	}

	lowerFontsDir := strings.ToLower(fontsDir)
	var orphaned []orphanedFontEntry
	for _, name := range names {
		val, _, err := k.GetStringValue(name)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(val), lowerFontsDir) {
			continue
		}
		base := filepath.Base(val)
		if expected[base] {
			continue
		}
		orphaned = append(orphaned, orphanedFontEntry{
			display:  base + ": missing manifest",
			filePath: val,
		})
	}
	return orphaned
}
