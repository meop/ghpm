package asset

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
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

var skipSuffixes = []string{
	".sha256", ".sha512", ".sig", ".pem", ".sbom",
	".deb", ".apk", ".rpm", ".msi", ".pkg",
}

var skipPatterns = []string{"src", "source"}

func isSkipped(name string) bool {
	lower := strings.ToLower(name)
	for _, suf := range skipSuffixes {
		if strings.HasSuffix(lower, suf) {
			return true
		}
	}
	for _, pat := range skipPatterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
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

// priorityResolved returns true if a clearly ranks higher than b by priority.
func priorityResolved(aName, bName string, cfg *config.Settings) bool {
	goos := runtime.GOOS
	priorities := cfg.PlatformPriority[goos]
	aRank, bRank := len(priorities), len(priorities) // default: unranked
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
	priorities, ok := cfg.PlatformPriority[goos]
	if !ok || len(priorities) == 0 {
		return assets
	}
	// Sort assets by priority: earlier match in priorities list = higher priority
	best := make([]gh.Asset, 0, len(assets))
	for _, pri := range priorities {
		for _, a := range assets {
			if strings.Contains(strings.ToLower(a.Name), strings.ToLower(pri)) {
				best = append(best, a)
			}
		}
	}
	// Append any that didn't match a priority
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

// SelectAsset picks the best asset for the current platform.
// hint is the previously stored asset_pattern (may be "").
// If multiple candidates exist, it prompts the user.
func SelectAsset(assets []gh.Asset, cfg *config.Settings, hint string) (gh.Asset, error) {
	// Filter out non-binaries
	candidates := make([]gh.Asset, 0, len(assets))
	for _, a := range assets {
		if !isSkipped(a.Name) {
			candidates = append(candidates, a)
		}
	}
	if len(candidates) == 0 {
		return gh.Asset{}, fmt.Errorf("no compatible assets found")
	}

	// If we have a hint, try to find an exact or prefix match
	if hint != "" {
		for _, a := range candidates {
			if a.Name == hint {
				return a, nil
			}
		}
	}

	// Score assets
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
		// No OS/arch match — show all and prompt
		return promptSelect("No auto-matched assets. Select one:", candidates)
	}

	// Find max score
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

	// Apply platform priority to break ties
	best = applyPriority(best, cfg)

	if len(best) == 1 {
		return best[0], nil
	}

	// If priority resolved the tie (first element matches a priority keyword),
	// auto-select rather than prompting.
	if priorityResolved(best[0].Name, best[1].Name, cfg) {
		return best[0], nil
	}

	return promptSelect("Multiple candidates found. Select one:", best)
}

func promptSelect(msg string, assets []gh.Asset) (gh.Asset, error) {
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

// VerifySHA downloads the .sha256 file (if present among release assets) and
// verifies the downloaded binary. owner/repo/tag identify the release for download.
func VerifySHA(owner, repo, tag, cacheDir, assetName string, assets []gh.Asset) error {
	var shaAssetName string
	for _, a := range assets {
		if a.Name == assetName+".sha256" || a.Name == assetName+".sha256sum" {
			shaAssetName = a.Name
			break
		}
	}
	if shaAssetName == "" {
		return nil // no SHA file in this release
	}

	shaPath := filepath.Join(cacheDir, shaAssetName)
	if _, err := os.Stat(shaPath); os.IsNotExist(err) {
		if err := gh.DownloadAsset(owner, repo, tag, shaAssetName, cacheDir); err != nil {
			return fmt.Errorf("downloading %s: %w", shaAssetName, err)
		}
	}

	expected, err := readSHAFile(shaPath, assetName)
	if err != nil {
		return err
	}

	actual, err := sha256File(filepath.Join(cacheDir, assetName))
	if err != nil {
		return err
	}

	if !strings.EqualFold(expected, actual) {
		return fmt.Errorf("SHA256 mismatch for %s: expected %s, got %s", assetName, expected, actual)
	}
	return nil
}

func readSHAFile(path, assetName string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 1 {
			// Format: <hash>  <filename> or just <hash>
			if len(parts) == 1 || strings.Contains(parts[1], assetName) {
				return parts[0], nil
			}
		}
	}
	return "", fmt.Errorf("could not find SHA256 for %s in %s", assetName, path)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
