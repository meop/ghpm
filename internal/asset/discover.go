package asset

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DiscoverPaths searches pkgDir for a binary whose name contains name,
// checking common locations: root, bin/, and one level of subdirs.
// If multiple matches are found the user is prompted to choose.
// Returns the bin dir relative to pkgDir and the binary filename, or empty strings if not found.
// Returns ErrSkip if the user enters 0 at the prompt.
func DiscoverPaths(pkgDir, name string) (binDir string, binaryName string, err error) {
	entries, readErr := os.ReadDir(pkgDir)
	if readErr != nil {
		return "", "", nil
	}

	type nameMatch struct {
		rel  string
		name string
	}
	var matches []nameMatch
	seen := map[string]bool{}
	lower := strings.ToLower(name)

	collectMatches := func(dir, rel string) {
		dirEntries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range dirEntries {
			if e.IsDir() {
				continue
			}
			base := strings.TrimSuffix(e.Name(), ".exe")
			if !strings.Contains(strings.ToLower(base), lower) {
				continue
			}
			path := filepath.Join(dir, e.Name())
			if seen[path] || !isBinaryFile(path) {
				continue
			}
			seen[path] = true
			ensureExecutable(path)
			matches = append(matches, nameMatch{rel: filepath.ToSlash(rel), name: base})
		}
	}

	for _, rel := range []string{"", "bin"} {
		collectMatches(filepath.Join(pkgDir, rel), rel)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		for _, rel := range []string{e.Name(), filepath.Join(e.Name(), "bin")} {
			collectMatches(filepath.Join(pkgDir, rel), rel)
		}
	}

	if len(matches) == 0 {
		return "", "", nil
	}
	if len(matches) == 1 {
		return matches[0].rel, matches[0].name, nil
	}
	fmt.Printf("choose which binary to use for %q:\n", name)
	for i, m := range matches {
		fmt.Printf("  %d) %s\n", i+1, m.name)
	}
	fmt.Print("enter number (0 to skip): ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(line)
	var idx int
	if _, scanErr := fmt.Sscanf(line, "%d", &idx); scanErr != nil {
		return "", "", nil
	}
	if idx == 0 {
		return "", "", ErrSkip
	}
	if idx < 1 || idx > len(matches) {
		return "", "", nil
	}
	return matches[idx-1].rel, matches[idx-1].name, nil
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
	var b [4]byte
	_, err = io.ReadFull(f, b[:])
	_ = f.Close()
	if err != nil {
		return false
	}
	return b[0] == 0x7f && b[1] == 'E' && b[2] == 'L' && b[3] == 'F'
}

func hasMachOMagic(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	var b [4]byte
	_, err = io.ReadFull(f, b[:])
	_ = f.Close()
	if err != nil {
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
