package asset

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var skipDirs = map[string]bool{
	"__macosx": true,
	".ds_store": true,
	".git":      true,
}

func DiscoverPaths(pkgDir string) (paths map[string][]string, binaryName string) {
	paths = map[string][]string{}
	hasBinDir := false

	_ = filepath.WalkDir(pkgDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		base := strings.ToLower(d.Name())
		if skipDirs[base] {
			return filepath.SkipDir
		}

		rel, err := filepath.Rel(pkgDir, path)
		if err != nil {
			return nil
		}
		if rel == "." {
			return nil
		}

		switch base {
		case "bin":
			paths["bin"] = append(paths["bin"], rel)
			hasBinDir = true
		case "lib":
			paths["lib"] = append(paths["lib"], rel)
		case "share":
			paths["share"] = append(paths["share"], rel)
		}
		return nil
	})

	visitManInShare(pkgDir, paths)

	if !hasBinDir {
		paths["bin"] = findExecutableDirs(pkgDir)
	}

	for _, binDir := range paths["bin"] {
		binaryName = findFirstExecutable(filepath.Join(pkgDir, binDir))
		if binaryName != "" {
			break
		}
	}
	if binaryName == "" {
		binaryName = findFirstExecutable(pkgDir)
	}

	return paths, binaryName
}

func visitManInShare(pkgDir string, paths map[string][]string) {
	shareDirs := paths["share"]
	paths["man"] = nil
	for _, sd := range shareDirs {
		absShare := filepath.Join(pkgDir, sd)
		manDir := filepath.Join(absShare, "man")
		if fi, err := os.Stat(manDir); err == nil && fi.IsDir() {
			rel, _ := filepath.Rel(pkgDir, manDir)
			paths["man"] = append(paths["man"], rel)
		}
	}
}

func findExecutableDirs(pkgDir string) []string {
	var dirs []string
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return nil
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
			dirs = []string{"."}
			return dirs
		}
	}

	var found []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(pkgDir, e.Name())
		if dirHasExecutables(sub) {
			rel, _ := filepath.Rel(pkgDir, sub)
			found = append(found, rel)
		}
	}
	if len(found) > 0 {
		return found
	}

	return []string{"."}
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
