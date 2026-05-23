package asset

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// BinaryCandidate is a discovered executable inside an extracted package dir.
type BinaryCandidate struct {
	BinDir  string // relative to pkgDir
	BinName string
}

// FindBinaries searches pkgDir for executables whose name contains name
// (case-insensitive), checking root, bin/, and one level of subdirs + their bin/.
func FindBinaries(pkgDir, name string) []BinaryCandidate {
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return nil
	}
	lower := strings.ToLower(name)
	seen := map[string]bool{}
	var matches []BinaryCandidate

	collect := func(dir, rel string) {
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
			matches = append(matches, BinaryCandidate{BinDir: filepath.ToSlash(rel), BinName: e.Name()})
		}
	}

	for _, rel := range []string{"", "bin"} {
		collect(filepath.Join(pkgDir, rel), rel)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		for _, rel := range []string{e.Name(), filepath.Join(e.Name(), "bin")} {
			collect(filepath.Join(pkgDir, rel), rel)
		}
	}
	return matches
}

// SelectBinaries returns the binaries the user wants to install.
//   - 0 candidates → nil, nil
//   - 1 candidate → auto-select
//   - Multiple: auto-select if candidate names exactly match prevNames;
//     otherwise prompt with yay-style multi-select (1,3-5 / empty=all / 0=skip)
func SelectBinaries(candidates []BinaryCandidate, name string, prevNames []string) ([]BinaryCandidate, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	if len(candidates) == 1 {
		return candidates, nil
	}

	candidateNames := make([]string, len(candidates))
	for i, c := range candidates {
		candidateNames[i] = c.BinName
	}
	if len(prevNames) > 0 && sameStringSet(candidateNames, prevNames) {
		return candidates, nil
	}

	fmt.Printf("%s: choose shim target(s)\n", name)
	for i, c := range candidates {
		fmt.Printf("  %d) %s\n", i+1, c.BinName)
	}
	fmt.Printf("enter number(s) (0 to skip, 1-%d or 1,x for multiple, empty=all): ", len(candidates))
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(line)

	indices, err := parseMultiSelect(line, len(candidates))
	if err != nil || indices == nil {
		return nil, ErrSkip
	}

	var selected []BinaryCandidate
	for _, idx := range indices {
		selected = append(selected, candidates[idx-1])
	}
	return selected, nil
}

// parseMultiSelect parses yay-style input: empty=all, 0=skip (nil,nil), e.g. "1,3-5".
func parseMultiSelect(input string, max int) ([]int, error) {
	if strings.TrimSpace(input) == "" {
		all := make([]int, max)
		for i := range all {
			all[i] = i + 1
		}
		return all, nil
	}

	seen := map[int]bool{}
	var result []int

	parts := strings.FieldsFunc(input, func(r rune) bool { return r == ',' || r == ' ' })
	for _, part := range parts {
		if strings.Contains(part, "-") {
			halves := strings.SplitN(part, "-", 2)
			from, e1 := strconv.Atoi(halves[0])
			to, e2 := strconv.Atoi(halves[1])
			if e1 != nil || e2 != nil || from < 1 || to > max || from > to {
				return nil, fmt.Errorf("invalid selection %q", part)
			}
			for i := from; i <= to; i++ {
				if !seen[i] {
					seen[i] = true
					result = append(result, i)
				}
			}
		} else {
			n, err := strconv.Atoi(part)
			if err != nil || n < 0 || n > max {
				return nil, fmt.Errorf("invalid selection %q", part)
			}
			if n == 0 {
				return nil, nil
			}
			if !seen[n] {
				seen[n] = true
				result = append(result, n)
			}
		}
	}

	slices.Sort(result)
	return result, nil
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := slices.Clone(a)
	bs := slices.Clone(b)
	slices.Sort(as)
	slices.Sort(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
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
