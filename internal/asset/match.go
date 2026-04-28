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

func tokenize(name string) []string {
	return strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
}

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

func scoreAsset(name string) int {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	score := 0

	if prefixes, ok := osPrefixes[goos]; ok && hasTokenPrefix(name, prefixes) {
		score += 10
	}
	if prefixes, ok := archPrefixes[goarch]; ok && hasTokenPrefix(name, prefixes) {
		score += 10
	}
	if goos == "windows" && strings.HasSuffix(strings.ToLower(name), ".exe") {
		score += 5
	}
	return score
}

func priorityResolved(aName, bName string, cfg *config.Settings) bool {
	goos := runtime.GOOS
	priorities := cfg.PlatPriority[goos]
	aRank, bRank := len(priorities), len(priorities)
	for i, pri := range priorities {
		if strings.Contains(strings.ToLower(aName), strings.ToLower(pri)) && i < aRank {
			aRank = i
		}
		if strings.Contains(strings.ToLower(bName), strings.ToLower(pri)) && i < bRank {
			bRank = i
		}
	}
	return aRank < bRank
}

func applyPriority(assets []gh.Asset, cfg *config.Settings) []gh.Asset {
	goos := runtime.GOOS
	priorities, ok := cfg.PlatPriority[goos]
	if !ok || len(priorities) == 0 {
		return assets
	}
	best := make([]gh.Asset, 0, len(assets))
	for _, pri := range priorities {
		for _, a := range assets {
			if strings.Contains(strings.ToLower(a.Name), strings.ToLower(pri)) {
				best = append(best, a)
			}
		}
	}
	inBest := map[string]bool{}
	for _, a := range best {
		inBest[a.Name] = true
	}
	for _, a := range assets {
		if !inBest[a.Name] {
			best = append(best, a)
		}
	}
	return best
}

type AssetCandidates struct {
	Chosen     gh.Asset
	Ambiguous []gh.Asset
	All       []gh.Asset
}

func SelectAssetAuto(assets []gh.Asset, cfg *config.Settings, hint string) (AssetCandidates, error) {
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

	type scored struct {
		asset gh.Asset
		score int
	}
	scored_ := make([]scored, 0, len(candidates))
	for _, a := range candidates {
		s := scoreAsset(a.Name)
		if s > 0 {
			scored_ = append(scored_, scored{a, s})
		}
	}

	if len(scored_) == 0 {
		return AssetCandidates{Ambiguous: candidates, All: candidates}, nil
	}

	maxScore := 0
	for _, s := range scored_ {
		if s.score > maxScore {
			maxScore = s.score
		}
	}
	best := make([]gh.Asset, 0)
	for _, s := range scored_ {
		if s.score == maxScore {
			best = append(best, s.asset)
		}
	}

	best = applyPriority(best, cfg)

	if len(best) == 1 {
		return AssetCandidates{Chosen: best[0], All: candidates}, nil
	}

	if len(best) >= 2 && priorityResolved(best[0].Name, best[1].Name, cfg) {
		return AssetCandidates{Chosen: best[0], All: candidates}, nil
	}

	return AssetCandidates{Ambiguous: best, All: candidates}, nil
}

func SelectAsset(assets []gh.Asset, cfg *config.Settings, hint string) (gh.Asset, error) {
	ac, err := SelectAssetAuto(assets, cfg, hint)
	if err != nil {
		return gh.Asset{}, err
	}
	if ac.Chosen.Name != "" {
		return ac.Chosen, nil
	}
	if len(ac.Ambiguous) > 0 {
		return PromptSelect("Multiple candidates found. Select one:", ac.Ambiguous)
	}
	return PromptSelect("No auto-matched assets. Select one:", ac.All)
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

func PromptSelect(msg string, assets []gh.Asset) (gh.Asset, error) {
	fmt.Println(msg)
	for i, a := range assets {
		fmt.Printf("  %d) %s (%d bytes)\n", i+1, a.Name, a.Size)
	}
	fmt.Print("Enter number: ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	var idx int
	if _, err := fmt.Sscanf(line, "%d", &idx); err != nil || idx < 1 || idx > len(assets) {
		return gh.Asset{}, fmt.Errorf("invalid selection")
	}
	return assets[idx-1], nil
}

func Verify(owner, repo, tag, cacheDir, assetName string) (bool, error) {
	assetPath := filepath.Join(cacheDir, assetName)
	if _, err := os.Stat(assetPath); os.IsNotExist(err) {
		return false, fmt.Errorf("asset not found: %s", assetPath)
	}

	cmd := exec.Command("gh", "release", "verify-asset", tag, assetPath, "-R", owner+"/"+repo)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "no attestation found") || strings.Contains(string(out), "not found") {
			return false, nil
		}
		return false, fmt.Errorf("verification failed: %s", string(out))
	}
	return true, nil
}