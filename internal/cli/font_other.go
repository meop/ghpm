//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"strings"
)

func registerFont(_, _ string) error { return nil }
func unregisterFont(_ string)        {}
func fontRegistered(_ string) bool   { return true }

func findOrphanedFonts(expected map[string]bool, fontsDir string) []orphanedFontEntry {
	entries, err := os.ReadDir(fontsDir)
	if err != nil {
		return nil
	}
	var orphaned []orphanedFontEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		switch strings.ToLower(filepath.Ext(name)) {
		case ".ttf", ".otf", ".woff", ".woff2":
		default:
			continue
		}
		if expected[name] {
			continue
		}
		orphaned = append(orphaned, orphanedFontEntry{
			display:  name + ": missing manifest",
			filePath: filepath.Join(fontsDir, name),
		})
	}
	return orphaned
}
