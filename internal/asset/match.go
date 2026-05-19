package asset

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
)

var osPrefixes = map[string][]string{
	"linux":   {"lin"},
	"darwin":  {"dar", "mac"},
	"windows": {"win"},
}

var archPrefixes = map[string][]string{
	"amd64": {"amd", "x64", "x86"},
	"arm64": {"arm", "aarch"},
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
		return r == '-' || r == '_' || r == ' '
	})
}

func tokenize(name string) []string { return Tokenize(name) }

func hasTokenPrefix(name string, prefixes []string) bool {
	for _, token := range tokenize(name) {
		for _, prefix := range prefixes {
			if strings.HasPrefix(token, prefix) {
				return true
			}
		}
	}
	return false
}

func scoreAsset(name, pkgName string) (int, bool) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	score := 0
	hasNeg := false

	if pkgName != "" && hasTokenPrefix(name, []string{pkgName}) {
		score++
	}

	if prefixes, ok := osPrefixes[goos]; ok && hasTokenPrefix(name, prefixes) {
		score++
	} else {
		for os, prefixes := range osPrefixes {
			if os != goos && hasTokenPrefix(name, prefixes) {
				hasNeg = true
				break
			}
		}
	}

	if prefixes, ok := archPrefixes[goarch]; ok && hasTokenPrefix(name, prefixes) {
		score++
	} else {
		for arch, prefixes := range archPrefixes {
			if arch != goarch && hasTokenPrefix(name, prefixes) {
				hasNeg = true
				break
			}
		}
	}

	if goos == "windows" && strings.HasSuffix(strings.ToLower(name), ".exe") {
		score++
	}

	return score, hasNeg
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
		asset  gh.Asset
		score  int
		hasNeg bool
	}
	all := make([]candidateScore, 0, len(candidates))
	for _, a := range candidates {
		s, neg := scoreAsset(a.Name, pkgName)
		all = append(all, candidateScore{a, s, neg})
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
	if len(workingSet) == 0 {
		workingSet = all
		hidden = nil
	}

	maxScore := 0
	for _, c := range workingSet {
		if c.score > maxScore {
			maxScore = c.score
		}
	}

	var best []gh.Asset
	for _, c := range workingSet {
		if maxScore == 0 || c.score == maxScore {
			best = append(best, c.asset)
		}
	}

	if len(best) == 1 {
		return AssetCandidates{Chosen: best[0], All: candidates}, nil
	}

	var hiddenAssets []gh.Asset
	for _, c := range hidden {
		hiddenAssets = append(hiddenAssets, c.asset)
	}

	return AssetCandidates{Compatible: best, Hidden: hiddenAssets, All: candidates}, nil
}

func SelectAsset(assets []gh.Asset, cfg *config.Settings, hint, pkgName string) (gh.Asset, error) {
	ac, err := SelectAssetAuto(assets, cfg, hint, pkgName)
	if err != nil {
		return gh.Asset{}, err
	}
	return PromptFromCandidates(ac)
}

func PromptFromCandidates(ac AssetCandidates) (gh.Asset, error) {
	if ac.Chosen.Name != "" {
		return ac.Chosen, nil
	}
	return promptWithShowMore(ac.Compatible, ac.Hidden)
}

func promptWithShowMore(compatible, hidden []gh.Asset) (gh.Asset, error) {
	fmt.Println("choose from candidates:")
	for i, a := range compatible {
		fmt.Printf("  %d) %s (%d bytes)\n", i+1, a.Name, a.Size)
	}
	showMoreIdx := -1
	if len(hidden) > 0 {
		showMoreIdx = len(compatible) + 1
		fmt.Printf("  %d) show more (%d more)\n", showMoreIdx, len(hidden))
	}
	fmt.Print("enter number: ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	var idx int
	if _, err := fmt.Sscanf(line, "%d", &idx); err != nil {
		return gh.Asset{}, fmt.Errorf("invalid selection")
	}
	if showMoreIdx > 0 && idx == showMoreIdx {
		return PromptSelect("choose from candidates:", append(compatible, hidden...))
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
	fmt.Print("enter number: ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	var idx int
	if _, err := fmt.Sscanf(line, "%d", &idx); err != nil || idx < 1 || idx > len(assets) {
		return gh.Asset{}, fmt.Errorf("invalid selection")
	}
	return assets[idx-1], nil
}

func matchByHint(candidates []gh.Asset, hint string) (gh.Asset, bool) {
	hintTokens := stripVersionTokens(tokenize(hint))
	if len(hintTokens) == 0 {
		return gh.Asset{}, false
	}

	var match gh.Asset
	matchCount := 0
	for _, a := range candidates {
		candidateTokens := stripVersionTokens(tokenize(a.Name))
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

func Verify(owner, repo, tag, cacheDir, assetName string) (bool, error) {
	assetPath := filepath.Join(cacheDir, assetName)
	if _, err := os.Stat(assetPath); os.IsNotExist(err) {
		return false, fmt.Errorf("asset not found: %s", assetPath)
	}

	cmd := exec.Command("gh", "release", "verify-asset", tag, assetPath, "-R", owner+"/"+repo)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "no attestation") || strings.Contains(string(out), "not found") {
			return false, nil
		}
		return false, fmt.Errorf("verification failed: %s", string(out))
	}
	return true, nil
}
