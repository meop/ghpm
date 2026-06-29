package asset

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/ui"
)

var ErrSkip = ui.ErrSkip

var ErrNoCompatibleAsset = errors.New("no compatible assets found")

var osNames = map[string][]string{
	"darwin":  {"darwin", "macos", "osx"},
	"linux":   {"linux"},
	"windows": {"windows"},
}

var archNames = map[string][]string{
	"arm64": {"arm64", "aarch64"},
	"amd64": {"amd64", "x86_64", "x64"},
}

var toolPrefs = map[string][]string{
	"darwin":  {},
	"linux":   {"gnu", "musl"},
	"windows": {"msvc", "gnu"},
}

var extValues = []string{
	"tar.gz", "tgz", "tar.bz2", "tar.xz", "zip",
}

var extPrefs = map[string][]string{
	"darwin":  extValues,
	"linux":   extValues,
	"windows": append(extValues[len(extValues)-1:], extValues[:len(extValues)-1]...),
}

func isSkipped(name string) bool {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "src") || strings.Contains(lower, "source") {
		return true
	}
	for _, suf := range extValues {
		if strings.HasSuffix(lower, "."+suf) {
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

func secondaryScore(name string) int {
	lower := strings.ToLower(name)
	goos := runtime.GOOS
	total := 0
	if prefs, ok := toolPrefs[goos]; ok {
		n := len(prefs)
		for i := n - 1; i >= 0; i-- {
			if p := prefs[i]; p != "" && strings.Contains(lower, p) {
				total += 1 + (n - 1 - i)
			}
		}
	}
	if prefs, ok := extPrefs[goos]; ok {
		n := len(prefs)
		for i := n - 1; i >= 0; i-- {
			if strings.HasSuffix(lower, "."+prefs[i]) {
				total += 1 + (n - 1 - i)
			}
		}
	}
	return total
}

func stripAssetExt(name string) string {
	lower := strings.ToLower(name)
	for _, ext := range extValues {
		if strings.HasSuffix(lower, "."+ext) {
			return lower[:len(lower)-len(ext)-1]
		}
	}
	return lower
}

func containsAnyOf(lower string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.Contains(lower, p) {
			return true
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
	lower := strings.ToLower(name)
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	var r scoreResult

	if pkgName != "" && strings.Contains(lower, strings.ToLower(pkgName)) {
		r.score++
	}

	if prefixes, ok := osNames[goos]; ok && containsAnyOf(lower, prefixes) {
		r.score++
		r.osMatch = true
	} else {
		for os, prefixes := range osNames {
			if os != goos && containsAnyOf(lower, prefixes) {
				r.hasNeg = true
				break
			}
		}
	}

	if prefixes, ok := archNames[goarch]; ok && containsAnyOf(lower, prefixes) {
		r.score++
		r.archMatch = true
	} else {
		for arch, prefixes := range archNames {
			if arch != goarch && containsAnyOf(lower, prefixes) {
				r.hasNeg = true
				break
			}
		}
	}

	if goos == "windows" && strings.HasSuffix(lower, ".exe") {
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
		return AssetCandidates{}, ErrNoCompatibleAsset
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

	slices.SortStableFunc(best, func(a, b candidateScore) int {
		if d := secondaryScore(b.asset.Name) - secondaryScore(a.asset.Name); d != 0 {
			return d
		}
		return strings.Compare(stripAssetExt(a.asset.Name), stripAssetExt(b.asset.Name))
	})

	seen := make(map[string]bool, len(best))
	deduped := best[:0]
	for _, c := range best {
		base := stripAssetExt(c.asset.Name)
		if seen[base] {
			hidden = append(hidden, c)
		} else {
			seen[base] = true
			deduped = append(deduped, c)
		}
	}
	best = deduped

	if len(best) == 1 && best[0].osMatch && best[0].archMatch {
		return AssetCandidates{Chosen: best[0].asset, All: candidates}, nil
	}

	slices.SortStableFunc(hidden, func(a, b candidateScore) int {
		return strings.Compare(stripAssetExt(a.asset.Name), stripAssetExt(b.asset.Name))
	})

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

func PromptFromCandidates(ac AssetCandidates, label string) (gh.Asset, error) {
	if ac.Chosen.Name != "" {
		return ac.Chosen, nil
	}
	// The Prompt bracket spans the whole interaction, including any show-more
	// expansion, so the inner helpers render + read without their own Break.
	return ui.Prompt(func() (gh.Asset, error) {
		return promptWithShowMore(ac.Compatible, ac.Hidden, label)
	})
}

// assetItems formats release assets as menu item bodies for ui.Menu.
func assetItems(assets []gh.Asset) []string {
	items := make([]string, len(assets))
	for i, a := range assets {
		items[i] = fmt.Sprintf("%s (%d bytes)", a.Name, a.Size)
	}
	return items
}

func promptWithShowMore(compatible, hidden []gh.Asset, label string) (gh.Asset, error) {
	items := assetItems(compatible)
	showMoreIdx := -1
	if len(hidden) > 0 {
		showMoreIdx = len(compatible) + 1
		items = append(items, fmt.Sprintf("show more (%d more)", len(hidden)))
	}
	ui.Menu(label, "choose asset:", items)
	idx, err := readSingle()
	if err != nil {
		return gh.Asset{}, err
	}
	if showMoreIdx > 0 && idx == showMoreIdx {
		return PromptSelect("choose asset:", append(compatible, hidden...), label)
	}
	if idx < 1 || idx > len(compatible) {
		return gh.Asset{}, fmt.Errorf("invalid selection")
	}
	return compatible[idx-1], nil
}

func PromptSelect(msg string, assets []gh.Asset, label string) (gh.Asset, error) {
	ui.Menu(label, msg, assetItems(assets))
	idx, err := readSingle()
	if err != nil {
		return gh.Asset{}, err
	}
	if idx < 1 || idx > len(assets) {
		return gh.Asset{}, fmt.Errorf("invalid selection")
	}
	return assets[idx-1], nil
}

// PromptAssetsMulti returns the auto-chosen asset (if unambiguous) or lets the
// user pick one or more from the candidate list.
func PromptAssetsMulti(ac AssetCandidates, label string) ([]gh.Asset, error) {
	if ac.Chosen.Name != "" {
		return []gh.Asset{ac.Chosen}, nil
	}
	return ui.Prompt(func() ([]gh.Asset, error) {
		return promptMultiWithShowMore(ac.Compatible, ac.Hidden, label)
	})
}

func promptMultiWithShowMore(compatible, hidden []gh.Asset, label string) ([]gh.Asset, error) {
	items := assetItems(compatible)
	showMoreIdx := -1
	if len(hidden) > 0 {
		showMoreIdx = len(compatible) + 1
		items = append(items, fmt.Sprintf("show more (%d more)", len(hidden)))
	}
	ui.Menu(label, "choose asset(s):", items)
	maxIdx := len(compatible)
	if showMoreIdx > 0 {
		maxIdx = showMoreIdx
	}
	indices, err := readMultiFirstWithShowMore(len(compatible), maxIdx)
	if err != nil {
		return nil, err
	}
	// If show-more was selected, re-prompt with the full list.
	for _, idx := range indices {
		if showMoreIdx > 0 && idx == showMoreIdx {
			return promptMultiAll(append(compatible, hidden...), label)
		}
	}
	var selected []gh.Asset
	for _, idx := range indices {
		if idx >= 1 && idx <= len(compatible) {
			selected = append(selected, compatible[idx-1])
		}
	}
	if len(selected) == 0 {
		return nil, ErrSkip
	}
	return selected, nil
}

func promptMultiAll(all []gh.Asset, label string) ([]gh.Asset, error) {
	ui.Menu(label, "choose asset(s):", assetItems(all))
	indices, err := readMultiFirst(len(all))
	if err != nil {
		return nil, err
	}
	var selected []gh.Asset
	for _, idx := range indices {
		selected = append(selected, all[idx-1])
	}
	return selected, nil
}

// ResolveByHint reports the asset that uniquely matches hint (a previously
// selected asset name), ignoring skipped assets. Unlike SelectAssetAuto it never
// falls back to platform scoring: a hit means the same asset was found in the new
// release, so callers re-resolving a prior selection can distinguish "carried
// over unchanged" from "had to guess". Returns ok=false when the hint matches
// zero or multiple assets.
func ResolveByHint(assets []gh.Asset, hint string) (gh.Asset, bool) {
	candidates := make([]gh.Asset, 0, len(assets))
	for _, a := range assets {
		if !isSkipped(a.Name) {
			candidates = append(candidates, a)
		}
	}
	return matchByHint(candidates, hint)
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
	s := t
	if strings.HasPrefix(s, "v") || strings.HasPrefix(s, "V") {
		s = s[1:]
	}
	return len(s) > 0 && s[0] >= '0' && s[0] <= '9'
}
