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
	BinDir  string // relative to pkgDir, always slash-separated
	BinName string
}

// Key returns the unique relative path for this candidate (BinDir/BinName, or just
// BinName when root-level). Used as the manifest Bins map key.
func (c BinaryCandidate) Key() string {
	if c.BinDir == "" {
		return c.BinName
	}
	return c.BinDir + "/" + c.BinName
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

// PromptShimRenames shows proposed shim names and lets the user rename them.
// Conflicting entries (reserved by another package OR duplicate) must be renamed before returning.
func PromptShimRenames(pkgName string, binKeys, proposed []string, reserved map[string]string) ([]string, error) {
	result := make([]string, len(proposed))
	copy(result, proposed)

	for {
		conflicts := make([]bool, len(result))
		anyConflict := false
		seen := make(map[string]bool, len(result))
		for i, name := range result {
			if _, inReserved := reserved[name]; inReserved {
				conflicts[i] = true
				anyConflict = true
			} else if seen[name] {
				conflicts[i] = true
				anyConflict = true
			} else {
				seen[name] = true
			}
		}

		fmt.Println()
		if anyConflict {
			fmt.Printf("%s: shim conflicts — rename required (0 to cancel):\n", pkgName)
		} else {
			fmt.Printf("%s: shim name(s) — enter number(s) to rename (empty to accept):\n", pkgName)
		}
		for i, shimName := range result {
			entry := fmt.Sprintf("  %d) %s", i+1, shimName)
			if binKeys[i] != shimName {
				entry += fmt.Sprintf("  [%s]", binKeys[i])
			}
			if owner, ok := reserved[shimName]; ok {
				entry += fmt.Sprintf("  ! already used by %s", owner)
			} else if conflicts[i] {
				entry += "  ! duplicate"
			}
			fmt.Println(entry)
		}
		fmt.Print("enter number(s): ")

		input, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "" && !anyConflict {
			break
		}

		var toRename []int
		if input == "" {
			for i, c := range conflicts {
				if c {
					toRename = append(toRename, i+1)
				}
			}
		} else {
			indices, err := parseMultiSelect(input, len(proposed))
			if err != nil || indices == nil {
				if anyConflict {
					fmt.Println("  cancelled")
					return nil, ErrSkip
				}
				break
			}
			toRename = indices
		}

		renamingIdx := make(map[int]bool, len(toRename))
		for _, idx := range toRename {
			renamingIdx[idx-1] = true
		}
		taken := make(map[string]bool, len(reserved)+len(result))
		for name := range reserved {
			taken[name] = true
		}
		for i, name := range result {
			if !renamingIdx[i] {
				taken[name] = true
			}
		}

		reader := bufio.NewReader(os.Stdin)
		for _, idx := range toRename {
			name := collectNewName(reader, idx, taken)
			if name == "" {
				if conflicts[idx-1] {
					return nil, ErrSkip
				}
				taken[result[idx-1]] = true
				continue
			}
			result[idx-1] = name
			taken[name] = true
		}

		if !anyConflict {
			break
		}
	}

	return result, nil
}

func collectNewName(reader *bufio.Reader, idx int, taken map[string]bool) string {
	for {
		fmt.Printf("  %d) new name: ", idx)
		name, _ := reader.ReadString('\n')
		name = strings.TrimSpace(name)
		if name == "" {
			return ""
		}
		if taken[name] {
			fmt.Printf("  %q is already taken, choose another\n", name)
			continue
		}
		return name
	}
}

// SelectBinaries returns the binaries the user wants to install.
//   - 0 candidates → nil, nil
//   - 1 candidate → auto-select
//   - Multiple: auto-select if candidate keys exactly match prevKeys;
//     otherwise prompt with yay-style multi-select (1,3-5 / empty=all / 0=skip)
func SelectBinaries(candidates []BinaryCandidate, name string, prevKeys []string) ([]BinaryCandidate, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	if len(candidates) == 1 {
		return candidates, nil
	}

	candidateKeys := make([]string, len(candidates))
	for i, c := range candidates {
		candidateKeys[i] = c.Key()
	}
	if len(prevKeys) > 0 && sameStringSet(candidateKeys, prevKeys) {
		return candidates, nil
	}

	fmt.Println()
	fmt.Printf("%s: choose shim target(s)\n", name)
	for i, c := range candidates {
		fmt.Printf("  %d) %s\n", i+1, c.Key())
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
