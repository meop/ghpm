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
