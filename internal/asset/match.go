package asset

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
)

var ErrSkip = errors.New("skipped")

var osPrefixes = map[string][]string{
	"darwin":  {"darwin", "macos", "osx"},
	"linux":   {"linux"},
	"windows": {"windows"},
}

var archPrefixes = map[string][]string{
	"arm64": {"arm64", "aarch64"},
	"amd64": {"amd64", "x86_64", "x64"},
}

var allowedSuffixes = []string{
	".tar.gz", ".tgz", ".tar.bz2", ".tar.xz", ".zip",
}

func isSkipped(name string) bool {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "src") || strings.Contains(lower, "source") {
		return true
	}
	for _, suf := range allowedSuffixes {
		if strings.HasSuffix(lower, suf) {
			return false
		}
	}
	return true
}

func Tokenize(name string) []string {
	return strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return r == '-' || r == ' '
	})
}

func hasTokenPrefix(name string, prefixes []string) bool {
	for _, token := range Tokenize(name) {
		for _, prefix := range prefixes {
			if strings.HasPrefix(token, prefix) {
				return true
			}
		}
		// also check underscore sub-segments (e.g. linux_amd64 → amd64)
		if strings.Contains(token, "_") {
			for _, sub := range strings.Split(token, "_") {
				for _, prefix := range prefixes {
					if strings.HasPrefix(sub, prefix) {
						return true
					}
				}
			}
		}
	}
	return false
}

type scoreResult struct {
	score     int
	hasNeg    bool
	osMatch   bool
	archMatch bool
}

func scoreAsset(name, pkgName string) scoreResult {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	var r scoreResult

	if pkgName != "" && hasTokenPrefix(name, []string{pkgName}) {
		r.score++
	}

	if prefixes, ok := osPrefixes[goos]; ok && hasTokenPrefix(name, prefixes) {
		r.score++
		r.osMatch = true
	} else {
		for os, prefixes := range osPrefixes {
			if os != goos && hasTokenPrefix(name, prefixes) {
				r.hasNeg = true
				break
			}
		}
	}

	if prefixes, ok := archPrefixes[goarch]; ok && hasTokenPrefix(name, prefixes) {
		r.score++
		r.archMatch = true
	} else {
		for arch, prefixes := range archPrefixes {
			if arch != goarch && hasTokenPrefix(name, prefixes) {
				r.hasNeg = true
				break
			}
		}
	}

	if goos == "windows" && strings.HasSuffix(strings.ToLower(name), ".exe") {
		r.score++
	}

	return r
}

type AssetCandidates struct {
	Chosen     gh.Asset
	Compatible []gh.Asset
	Hidden     []gh.Asset
	All        []gh.Asset
}

func SelectAssetAuto(assets []gh.Asset, cfg *config.Settings, hint, pkgName string) (AssetCandidates, error) {
	candidates := make([]gh.Asset, 0, len(assets))
	for _, a := range assets {
		if !isSkipped(a.Name) {
			candidates = append(candidates, a)
		}
	}
	if len(candidates) == 0 {
		return AssetCandidates{}, fmt.Errorf("no compatible assets found")
	}

	if hint != "" {
		if chosen, ok := matchByHint(candidates, hint); ok {
			return AssetCandidates{Chosen: chosen, All: candidates}, nil
		}
	}

	type candidateScore struct {
		asset     gh.Asset
		score     int
		hasNeg    bool
		osMatch   bool
		archMatch bool
	}
	all := make([]candidateScore, 0, len(candidates))
	for _, a := range candidates {
		sr := scoreAsset(a.Name, pkgName)
		all = append(all, candidateScore{a, sr.score, sr.hasNeg, sr.osMatch, sr.archMatch})
	}

	var compatible, hidden []candidateScore
	for _, c := range all {
		if c.hasNeg {
			hidden = append(hidden, c)
		} else {
			compatible = append(compatible, c)
		}
	}

	workingSet := compatible

	maxScore := 0
	for _, c := range workingSet {
		if c.score > maxScore {
			maxScore = c.score
		}
	}

	var best []candidateScore
	for _, c := range workingSet {
		if maxScore == 0 || c.score == maxScore {
			best = append(best, c)
		}
	}

	if len(best) == 1 && best[0].osMatch && best[0].archMatch {
		return AssetCandidates{Chosen: best[0].asset, All: candidates}, nil
	}

	var bestAssets []gh.Asset
	for _, c := range best {
		bestAssets = append(bestAssets, c.asset)
	}
	var hiddenAssets []gh.Asset
	for _, c := range hidden {
		hiddenAssets = append(hiddenAssets, c.asset)
	}

	return AssetCandidates{Compatible: bestAssets, Hidden: hiddenAssets, All: candidates}, nil
}

func PromptFromCandidates(ac AssetCandidates) (gh.Asset, error) {
	if ac.Chosen.Name != "" {
		return ac.Chosen, nil
	}
	return promptWithShowMore(ac.Compatible, ac.Hidden)
}

func promptWithShowMore(compatible, hidden []gh.Asset) (gh.Asset, error) {
	fmt.Println("choose asset:")
	for i, a := range compatible {
		fmt.Printf("  %d) %s (%d bytes)\n", i+1, a.Name, a.Size)
	}
	showMoreIdx := -1
	if len(hidden) > 0 {
		showMoreIdx = len(compatible) + 1
		fmt.Printf("  %d) show more (%d more)\n", showMoreIdx, len(hidden))
	}
	fmt.Print("enter number (0=skip): ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	var idx int
	if _, err := fmt.Sscanf(line, "%d", &idx); err != nil {
		return gh.Asset{}, fmt.Errorf("invalid selection")
	}
	if idx == 0 {
		return gh.Asset{}, ErrSkip
	}
	if showMoreIdx > 0 && idx == showMoreIdx {
		return PromptSelect("choose asset:", append(compatible, hidden...))
	}
	if idx < 1 || idx > len(compatible) {
		return gh.Asset{}, fmt.Errorf("invalid selection")
	}
	return compatible[idx-1], nil
}

func PromptSelect(msg string, assets []gh.Asset) (gh.Asset, error) {
	fmt.Println(msg)
	for i, a := range assets {
		fmt.Printf("  %d) %s (%d bytes)\n", i+1, a.Name, a.Size)
	}
	fmt.Print("enter number (0=skip): ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	var idx int
	if _, err := fmt.Sscanf(line, "%d", &idx); err != nil {
		return gh.Asset{}, fmt.Errorf("invalid selection")
	}
	if idx == 0 {
		return gh.Asset{}, ErrSkip
	}
	if idx < 1 || idx > len(assets) {
		return gh.Asset{}, fmt.Errorf("invalid selection")
	}
	return assets[idx-1], nil
}

func matchByHint(candidates []gh.Asset, hint string) (gh.Asset, bool) {
	hintTokens := stripVersionTokens(Tokenize(hint))
	if len(hintTokens) == 0 {
		return gh.Asset{}, false
	}

	var match gh.Asset
	matchCount := 0
	for _, a := range candidates {
		candidateTokens := stripVersionTokens(Tokenize(a.Name))
		if tokensMatch(hintTokens, candidateTokens) {
			match = a
			matchCount++
		}
	}
	if matchCount == 1 {
		return match, true
	}
	return gh.Asset{}, false
}

func tokensMatch(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func stripVersionTokens(tokens []string) []string {
	filtered := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if isVersionToken(t) {
			continue
		}
		filtered = append(filtered, t)
	}
	return filtered
}

func IsVersionToken(t string) bool { return isVersionToken(t) }

func isVersionToken(t string) bool {
	hasDigit := false
	for _, r := range t {
		if r >= '0' && r <= '9' {
			hasDigit = true
		}
	}
	if !hasDigit {
		return false
	}
	s := t
	if strings.HasPrefix(s, "v") || strings.HasPrefix(s, "V") {
		s = s[1:]
	}
	if len(s) > 0 && s[0] >= '0' && s[0] <= '9' {
		return true
	}
	return false
}
