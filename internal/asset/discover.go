package asset

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// DiscoverPaths searches pkgDir for a binary named name (or name.exe on Windows),
// checking common locations: root, bin/, and one level of subdirs.
// Returns the bin dir relative to pkgDir and the binary name, or empty strings if not found.
func DiscoverPaths(pkgDir, name string) (binDir string, binaryName string) {
	target := name
	if runtime.GOOS == "windows" {
		target = name + ".exe"
	}

	// Check root, then bin/ at root
	for _, rel := range []string{"", "bin"} {
		if isBinaryFile(filepath.Join(pkgDir, rel, target)) {
			ensureExecutable(filepath.Join(pkgDir, rel, target))
			return rel, name
		}
	}

	// Check one level of subdirs, then bin/ within each
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return "", ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		for _, rel := range []string{e.Name(), filepath.Join(e.Name(), "bin")} {
			if isBinaryFile(filepath.Join(pkgDir, rel, target)) {
				ensureExecutable(filepath.Join(pkgDir, rel, target))
				return filepath.ToSlash(rel), name
			}
		}
	}

	return "", ""
}

func isBinaryFile(path string) bool {
	switch runtime.GOOS {
	case "windows":
		_, err := os.Stat(path)
		return err == nil
	case "darwin":
		return hasMachOMagic(path)
	default:
		return hasELFMagic(path)
	}
}

func hasELFMagic(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var b [4]byte
	if _, err := io.ReadFull(f, b[:]); err != nil {
		return false
	}
	return b[0] == 0x7f && b[1] == 'E' && b[2] == 'L' && b[3] == 'F'
}

func hasMachOMagic(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var b [4]byte
	if _, err := io.ReadFull(f, b[:]); err != nil {
		return false
	}
	// FAT universal, LE 32-bit, LE 64-bit, BE 32-bit/64-bit
	return (b[0] == 0xca && b[1] == 0xfe && b[2] == 0xba && b[3] == 0xbe) ||
		(b[0] == 0xce && b[1] == 0xfa && b[2] == 0xed && b[3] == 0xfe) ||
		(b[0] == 0xcf && b[1] == 0xfa && b[2] == 0xed && b[3] == 0xfe) ||
		(b[0] == 0xfe && b[1] == 0xed && b[2] == 0xfa && (b[3] == 0xce || b[3] == 0xcf))
}

func ensureExecutable(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if info.Mode()&0111 == 0 {
		_ = os.Chmod(path, info.Mode()|0755)
	}
}
