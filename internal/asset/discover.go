package asset

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func DiscoverPaths(pkgDir string) (binPath string, binaryName string) {
	binPath = findBinDir(pkgDir)
	binaryName = findFirstExecutable(filepath.Join(pkgDir, binPath))
	if binaryName == "" && binPath != "" {
		binaryName = findFirstExecutable(pkgDir)
	}
	return binPath, binaryName
}

func findBinDir(pkgDir string) string {
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return ""
	}

	for _, e := range entries {
		if e.IsDir() && strings.ToLower(e.Name()) == "bin" {
			return e.Name()
		}
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if isExecutableFile(info, e.Name()) {
			return ""
		}
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(pkgDir, e.Name())
		if dirHasExecutables(sub) {
			return e.Name()
		}
	}

	return ""
}

func dirHasExecutables(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if isExecutableFile(info, e.Name()) {
			return true
		}
	}
	return false
}

func findFirstExecutable(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if isExecutableFile(info, e.Name()) {
			return strings.TrimSuffix(e.Name(), ".exe")
		}
	}
	return ""
}

func isExecutableFile(info os.FileInfo, name string) bool {
	if runtime.GOOS == "windows" {
		lower := strings.ToLower(name)
		return strings.HasSuffix(lower, ".exe") || info.Mode()&0111 != 0
	}
	return info.Mode()&0111 != 0
}
