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
	"darwin":  {"darwin", "macos", "mac", "osx"},
	"linux":   {"linux", "lin"},
	"windows": {"windows", "win"},
}

var archNames = map[string][]string{
	"amd64": {"amd64", "x86_64", "x64"},
	"arm64": {"arm64", "aarch64"},
}

var toolPrefs = map[string][]string{
	"darwin":  {},
	"linux":   {"gnu", "musl"},
	"windows": {"msvc", "gnu"},
}

// extValues is every archive/compression suffix ghpm recognizes, in
// unix-preference order: tar formats first (native there), 7z/zip last
// (native on Windows instead — see extPrefs).
var extValues = []string{
	"tar.gz", "tgz", "gz",
	"tar.xz", "txz", "xz",
	"tar.zst", "tzst", "zst",
	"tar.bz2", "tbz2", "bz2",
	"tar",
	"7z",
	"zip",
}

// extPrefs is the per-OS tie-break order secondaryScore weighs archive/
// compression suffixes by. unix keeps extValues as authored (tar formats
// first, since that's what unix decodes natively); windows is simply that
// same order reversed (zip/7z first, since those are native there) rather
// than a second hand-maintained list.
var extPrefs = map[string][]string{
	"darwin":  extValues,
	"linux":   extValues,
	"windows": reverseStrings(extValues),
}

func reverseStrings(s []string) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[len(s)-1-i] = v
	}
	return out
}

// isAlpha is the only character class hasDelimitedSubstring treats as
// "continuing a word".
func isAlpha(b byte) bool {
	return b >= 'a' && b <= 'z'
}

// hasDelimitedSubstring reports whether sub appears in s (both assumed
// lowercase) as a delimited segment rather than a bare substring — e.g. "win"
// matches "-win-" and "win32" but not "darwin". The one boundary primitive
// every name/prefix check in this file shares.
func hasDelimitedSubstring(s, sub string) bool {
	if sub == "" {
		return false
	}
	for start := 0; ; {
		idx := strings.Index(s[start:], sub)
		if idx == -1 {
			return false
		}
		idx += start
		end := idx + len(sub)
		beforeOK := idx == 0 || !isAlpha(s[idx-1])
		afterOK := end == len(s) || !isAlpha(s[end])
		if beforeOK && afterOK {
			return true
		}
		start = idx + 1
	}
}

// containsAnyOf reports whether any prefix appears in lower as a delimited
// segment (see hasDelimitedSubstring).
func containsAnyOf(lower string, prefixes []string) bool {
	for _, p := range prefixes {
		if hasDelimitedSubstring(lower, p) {
			return true
		}
	}
	return false
}

// countDelimitedSubstring reports how many non-overlapping delimited
// occurrences of sub appear in s (see hasDelimitedSubstring).
func countDelimitedSubstring(s, sub string) int {
	if sub == "" {
		return 0
	}
	count := 0
	for start := 0; ; {
		idx := strings.Index(s[start:], sub)
		if idx == -1 {
			return count
		}
		idx += start
		end := idx + len(sub)
		beforeOK := idx == 0 || !isAlpha(s[idx-1])
		afterOK := end == len(s) || !isAlpha(s[end])
		if beforeOK && afterOK {
			count++
		}
		start = idx + 1
	}
}

// weightedMatchScore sums positional weight for every delimited hit of any
// prefix in lower — prefixes[0] is worth len(prefixes) points, the last
// entry is worth 1 (same scheme as toolPrefs/extPrefs). A name mentioning
// several aliases (or the same one more than once) scores higher than one
// with a single hint, since each hit adds its own weight.
func weightedMatchScore(lower string, prefixes []string) int {
	n := len(prefixes)
	total := 0
	for i, p := range prefixes {
		if c := countDelimitedSubstring(lower, p); c > 0 {
			total += c * (n - i)
		}
	}
	return total
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
			if p := prefs[i]; p != "" && hasDelimitedSubstring(lower, p) {
				total += 1 + (n - 1 - i)
			}
		}
	}
	if prefs, ok := extPrefs[goos]; ok {
		// Longest match wins, not a sum — "tar.gz" also ends in "gz", so
		// summing every matching suffix would double count it against the
		// bare entry.
		n := len(prefs)
		bestLen := -1
		bestWeight := 0
		for i, p := range prefs {
			if strings.HasSuffix(lower, "."+p) && len(p) > bestLen {
				bestLen = len(p)
				bestWeight = 1 + (n - 1 - i)
			}
		}
		total += bestWeight
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

type scoreResult struct {
	score     int
	hasNeg    bool
	osMatch   bool
	archMatch bool
}

// matchPlatformSignal scores how strongly lower matches hostKey in names
// (osNames/archNames) — see weightedMatchScore. If hostKey has no entry
// (unlisted GOOS/GOARCH), no known-platform token at all counts as a single
// point, matched by elimination since nothing contradicts it.
func matchPlatformSignal(lower, hostKey string, names map[string][]string) (score int, hasNeg bool) {
	if prefixes, ok := names[hostKey]; ok {
		if s := weightedMatchScore(lower, prefixes); s > 0 {
			return s, false
		}
		for key, other := range names {
			if key != hostKey && containsAnyOf(lower, other) {
				return 0, true
			}
		}
		return 0, false
	}
	for _, prefixes := range names {
		if containsAnyOf(lower, prefixes) {
			return 0, true
		}
	}
	return 1, false
}

func scoreAsset(name, pkgName string) scoreResult {
	lower := strings.ToLower(name)
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	var r scoreResult

	if pkgName != "" && hasDelimitedSubstring(lower, strings.ToLower(pkgName)) {
		r.score++
	}

	if s, neg := matchPlatformSignal(lower, goos, osNames); s > 0 {
		r.score += s
		r.osMatch = true
	} else if neg {
		r.hasNeg = true
	}

	if s, neg := matchPlatformSignal(lower, goarch, archNames); s > 0 {
		r.score += s
		r.archMatch = true
	} else if neg {
		r.hasNeg = true
	}

	if hasRecognizedExt(lower) {
		r.score++
	}

	return r
}

// hasRecognizedExt reports whether lower ends in a known archive/compression
// suffix or ".exe". Checksum sidecars don't, which is what keeps them from
// tying scoreAsset's score with their target.
func hasRecognizedExt(lower string) bool {
	for _, ext := range extValues {
		if strings.HasSuffix(lower, "."+ext) {
			return true
		}
	}
	return strings.HasSuffix(lower, ".exe")
}

type AssetCandidates struct {
	Chosen     gh.Asset
	Compatible []gh.Asset
	Hidden     []gh.Asset
	All        []gh.Asset
}

func SelectAssetAuto(assets []gh.Asset, cfg *config.Settings, hint, pkgName string) (AssetCandidates, error) {
	if len(assets) == 0 {
		return AssetCandidates{}, ErrNoCompatibleAsset
	}
	candidates := assets

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
// selected asset name). Unlike SelectAssetAuto it never falls back to platform
// scoring: a hit means the same asset was found in the new release, so callers
// re-resolving a prior selection can distinguish "carried over unchanged" from
// "had to guess". Returns ok=false when the hint matches zero or multiple assets.
func ResolveByHint(assets []gh.Asset, hint string) (gh.Asset, bool) {
	return matchByHint(assets, hint)
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
