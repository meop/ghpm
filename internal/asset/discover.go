package asset

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/meop/ghpm/internal/ioutils"
)

// BinCandidate is a discovered executable inside an extracted package dir.
type BinCandidate struct {
	BinDir  string // relative to pkgDir, always slash-separated
	BinName string
}

// Key returns the unique relative path for this candidate (BinDir/BinName, or just
// BinName when root-level). Used as the manifest Bins map key.
func (c BinCandidate) Key() string {
	if c.BinDir == "" {
		return c.BinName
	}
	return c.BinDir + "/" + c.BinName
}

func (c BinCandidate) label() string { return c.BinName }

// FindBins searches pkgDir for executables whose name contains name
// (case-insensitive), checking root, bin/, and one level of subdirs + their bin/.
func FindBins(pkgDir, name string) []BinCandidate {
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return nil
	}
	lower := strings.ToLower(name)
	seen := map[string]bool{}
	var matches []BinCandidate

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
			matches = append(matches, BinCandidate{BinDir: filepath.ToSlash(rel), BinName: e.Name()})
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

// PromptBinNames shows proposed shim names and lets the user rename them.
// Conflicting entries (reserved by another package OR duplicate) are always renamed.
// When conflicts exist, the user is also asked if they want to rename any additional entries.
func PromptBinNames(binKeys, proposed []string, reserved map[string]string) ([]string, error) {
	return promptNameConflicts(binKeys, proposed, reserved, "bin name(s)", "bin conflicts — rename required:")
}

// PromptFontConflicts resolves name conflicts for an already-computed font name mapping.
// fontKeys and proposed are parallel slices (font file paths and current user-given names).
func PromptFontConflicts(fontKeys, proposed []string, reserved map[string]string) ([]string, error) {
	return promptNameConflicts(fontKeys, proposed, reserved, "font name(s)", "font name conflicts — rename required:")
}

// promptNameConflicts is the shared rename-conflict engine used by both PromptBinNames
// and PromptFontConflicts. keys are displayed alongside proposed names for identification.
func promptNameConflicts(keys, proposed []string, reserved map[string]string, headerOK, headerConflict string) ([]string, error) {
	result := make([]string, len(proposed))
	copy(result, proposed)

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

	if anyConflict {
		fmt.Println(headerConflict)
	} else {
		fmt.Println(headerOK)
	}
	for i, name := range result {
		entry := fmt.Sprintf("  %d) %s", i+1, name)
		entry += fmt.Sprintf("  [%s]", keys[i])
		if owner, ok := reserved[name]; ok {
			entry += fmt.Sprintf("  ! already used by %s", owner)
		} else if conflicts[i] {
			entry += "  ! duplicate"
		}
		fmt.Println(entry)
	}

	var toRename []int
	if anyConflict {
		for i, c := range conflicts {
			if c {
				toRename = append(toRename, i+1)
			}
		}
		nonConflictCount := len(proposed) - len(toRename)
		if nonConflictCount > 0 {
			additional, err := readMultiOptional("to also rename", len(proposed))
			if err != nil {
				return nil, ErrSkip
			}
			if additional != nil {
				inToRename := make(map[int]bool, len(toRename))
				for _, idx := range toRename {
					inToRename[idx] = true
				}
				for _, idx := range additional {
					if !inToRename[idx] {
						toRename = append(toRename, idx)
						inToRename[idx] = true
					}
				}
				slices.Sort(toRename)
			}
		}
	} else {
		optional, err := readMultiOptional("to rename", len(proposed))
		if err != nil {
			return nil, ErrSkip
		}
		toRename = optional
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

	for _, idx := range toRename {
		var extra string
		if conflicts[idx-1] {
			extra = fmt.Sprintf(" %s [%s] (required due to conflict)", result[idx-1], keys[idx-1])
		} else {
			extra = fmt.Sprintf(" %s [%s]", result[idx-1], keys[idx-1])
		}
		name := collectNewName(extra, taken)
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

	return result, nil
}

func collectNewName(extra string, taken map[string]bool) string {
	for {
		name := ioutils.ReadLine(fmt.Sprintf("  rename%s: ", extra))
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

type selectCandidate interface {
	Key() string
	label() string
}

// selectItems is the shared selection logic for bins and fonts:
//   - 0 candidates → nil, nil
//   - 1 candidate → auto-select
//   - Multiple: auto-select if candidate keys exactly match prevKeys;
//     otherwise prompt with yay-style multi-select
func selectItems[C selectCandidate](candidates []C, prevKeys []string, noun string) ([]C, error) {
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
	fmt.Printf("choose %s\n", noun)
	for i, c := range candidates {
		entry := fmt.Sprintf("  %d) %s", i+1, c.label())
		if c.Key() != c.label() {
			entry += fmt.Sprintf("  [%s]", c.Key())
		}
		fmt.Println(entry)
	}
	indices, err := readMultiAll(len(candidates))
	if err != nil {
		return nil, err
	}
	var selected []C
	for _, idx := range indices {
		selected = append(selected, candidates[idx-1])
	}
	return selected, nil
}

func SelectBins(candidates []BinCandidate, prevKeys []string) ([]BinCandidate, error) {
	return selectItems(candidates, prevKeys, "bin(s)")
}

// parseMultiSelect parses yay-style input
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
		return strings.HasSuffix(strings.ToLower(path), ".exe")
	case "darwin":
		return hasMachOMagic(path)
	default:
		return hasELFMagic(path)
	}
}

func readMagicBytes(path string) ([4]byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return [4]byte{}, false
	}
	var b [4]byte
	_, err = io.ReadFull(f, b[:])
	_ = f.Close()
	return b, err == nil
}

func hasELFMagic(path string) bool {
	b, ok := readMagicBytes(path)
	return ok && b[0] == 0x7f && b[1] == 'E' && b[2] == 'L' && b[3] == 'F'
}

func hasMachOMagic(path string) bool {
	b, ok := readMagicBytes(path)
	return ok && ((b[0] == 0xca && b[1] == 0xfe && b[2] == 0xba && b[3] == 0xbe) ||
		(b[0] == 0xce && b[1] == 0xfa && b[2] == 0xed && b[3] == 0xfe) ||
		(b[0] == 0xcf && b[1] == 0xfa && b[2] == 0xed && b[3] == 0xfe) ||
		(b[0] == 0xfe && b[1] == 0xed && b[2] == 0xfa && (b[3] == 0xce || b[3] == 0xcf)))
}

// FontCandidate is a discovered font file inside an extracted package dir.
type FontCandidate struct {
	FontDir  string
	FontName string
}

func (c FontCandidate) label() string { return c.FontName }

func (c FontCandidate) Key() string {
	if c.FontDir == "" {
		return c.FontName
	}
	return c.FontDir + "/" + c.FontName
}

var fontSuffixes = []string{".ttf", ".otf", ".woff", ".woff2"}

// FindFonts walks pkgDir and returns all font files found.
func FindFonts(pkgDir string) []FontCandidate {
	var matches []FontCandidate
	_ = filepath.WalkDir(pkgDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		baseLower := strings.ToLower(d.Name())
		hasFontExt := false
		for _, suf := range fontSuffixes {
			if strings.HasSuffix(baseLower, suf) {
				hasFontExt = true
				break
			}
		}
		if !hasFontExt {
			return nil
		}
		rel, err := filepath.Rel(pkgDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "." {
			dir = ""
		}
		matches = append(matches, FontCandidate{FontDir: dir, FontName: d.Name()})
		return nil
	})
	return matches
}

// DeriveFontName returns a default install name for a font file.
func DeriveFontName(filename string) string {
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	return strings.ToLower(strings.ReplaceAll(name, " ", "-"))
}

// PromptFontNames derives default names for selected font files, checks for conflicts
// against reserved (other packages' font names), and lets the user rename as needed.
// Returns {fontName → fontFilePath} and any error.
func PromptFontNames(selected []FontCandidate, reserved map[string]string) (map[string]string, error) {
	if len(selected) == 0 {
		return nil, nil
	}
	names := make([]string, len(selected))
	seen := map[string]int{}
	for i, c := range selected {
		base := DeriveFontName(c.FontName)
		if n, ok := seen[base]; ok {
			seen[base] = n + 1
			names[i] = fmt.Sprintf("%s-%d", base, n+1)
		} else {
			seen[base] = 1
			names[i] = base
		}
	}
	keys := make([]string, len(selected))
	for i, c := range selected {
		keys[i] = c.Key()
	}
	renamed, err := promptNameConflicts(keys, names, reserved, "font name(s)", "font name conflicts — rename required:")
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(selected))
	for i, c := range selected {
		result[renamed[i]] = c.Key()
	}
	return result, nil
}

func SelectFonts(candidates []FontCandidate, prevKeys []string) ([]FontCandidate, error) {
	return selectItems(candidates, prevKeys, "font(s)")
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
