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
	"github.com/meop/ghpm/internal/version"
)

var ErrSkip = ui.ErrSkip

var ErrNoCompatibleAsset = errors.New("no compatible assets found")

var osNames = map[string][]string{
	"darwin":  {"darwin", "macos", "mac", "osx"},
	"linux":   {"linux", "lin"},
	"windows": {"windows", "win"},
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
// matches "-win-" and "win32" but not "darwin". Used only by secondaryScore's
// toolPrefs/extPrefs tie-break; pkgName/OS/arch matching instead goes through
// the exact-token machinery below (see scoreAsset), since a boundary that
// merely isn't a letter (a digit counts too) is too permissive there — it let
// "amd64codex" register a pkgName match for "codex" with no real delimiter
// between them at all.
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

// trimDigits trims leading and trailing digit runs from s, leaving any
// digits in the middle alone — "win32" → "win", but "x86_64" is untouched by
// a leading trim (starts with a letter) and only loses its trailing "64" to
// a trailing trim, landing on "x86_" either way, not a real word.
func trimDigits(s string) string {
	start := 0
	for start < len(s) && s[start] >= '0' && s[start] <= '9' {
		start++
	}
	end := len(s)
	for end > start && s[end-1] >= '0' && s[end-1] <= '9' {
		end--
	}
	return s[start:end]
}

// tokenEquals reports whether t equals target exactly, either as given or
// with leading/trailing digits trimmed (see trimDigits) — the same digit-trim
// rule applied uniformly everywhere a token is compared against a known name:
// "win32" sanitizes to "win" and matches the "win" OS alias; "codex123"
// sanitizes to "codex" and matches pkgName "codex". Not a boundary/substring
// check — an exact match against the token as a whole, once digit noise at
// its edges is set aside.
func tokenEquals(t, target string) bool {
	return target != "" && (t == target || trimDigits(t) == target)
}

// pkgTokenMatches reports whether t is pkgName as a whole word (see
// tokenEquals) — pkgTokens is Tokenize(pkgName), split the same way any
// other multi-word name would be.
func pkgTokenMatches(t string, pkgTokens []string) bool {
	for _, p := range pkgTokens {
		if tokenEquals(t, p) {
			return true
		}
	}
	return false
}

// matchAlias reports whether t (see tokenEquals) is a known alias in names,
// and if so which key it belongs to and how many points it's worth in that
// key's alias list — earlier entries are worth more, since they're the
// canonical spelling ("amd64" outweighs "x86_64" outweighs "x64"). Alias
// lists never overlap across keys, so which key is found first when names is
// ranged over doesn't matter.
func matchAlias(t string, names map[string][]string) (key string, weight int, ok bool) {
	for k, aliases := range names {
		for i, alias := range aliases {
			if tokenEquals(t, alias) {
				return k, len(aliases) - i, true
			}
		}
	}
	return "", 0, false
}

// isKnownAliasToken reports whether t is exactly a known OS, arch, or
// toolchain alias (osNames/archNames/toolPrefs), checked via matchAlias.
func isKnownAliasToken(t string) bool {
	if _, _, ok := matchAlias(t, osNames); ok {
		return true
	}
	if _, _, ok := matchAlias(t, archNames); ok {
		return true
	}
	if _, _, ok := matchAlias(t, toolPrefs); ok {
		return true
	}
	return false
}

// isStrictVersionToken reports whether t, in its entirety, looks like a bare
// version/build token — optional leading "v"/"V", then nothing but digits
// and "." for the rest. Stricter than isVersionToken (which only checks the
// first character — fine for a token Tokenize has already cleanly bounded on
// "-"/space, but not safe here: looseTokenize's greedy search tries much
// longer candidate spans, and a loose check would let "1.0-darwin-amd64"
// pass as one giant "version", silently swallowing the OS/arch tokens after
// it.
func isStrictVersionToken(t string) bool {
	s := t
	if strings.HasPrefix(s, "v") || strings.HasPrefix(s, "V") {
		s = s[1:]
	}
	if s == "" || s[0] < '0' || s[0] > '9' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; c != '.' && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// isTokenDelim reports whether b is one of the four characters looseTokenize
// treats as a field separator: space, "-", "_", ".".
func isTokenDelim(b byte) bool {
	return b == ' ' || b == '-' || b == '_' || b == '.'
}

// tokenBoundaries returns every candidate token-end position in name — each
// delimiter's index, in ascending order, plus len(name) itself.
func tokenBoundaries(name string) []int {
	bounds := make([]int, 0, 8)
	for i := 0; i < len(name); i++ {
		if isTokenDelim(name[i]) {
			bounds = append(bounds, i)
		}
	}
	return append(bounds, len(name))
}

// looseTokenize segments name by trying every one of the four delimiters
// (space, "-", "_", ".") together, not one fixed split — at each position it
// prefers the LONGEST span bounded by any of them that resolves to a known
// token (pkgName, a version, or a platform/toolchain alias), only shrinking
// to a smaller span when nothing longer matches, and finally falling back to
// a single plain (possibly "extra") token when nothing matches at all. This
// is what lets a compound alias survive intact regardless of which delimiter
// happens to border it, in either direction: "bottom_x86_64-pc-windows-msvc"
// (bottom's real release asset shape) recognizes "bottom" then "x86_64"
// whole even though they're joined by "_" while the rest of the name uses
// "-"; "claude-win32_x64.zip" (claude-code's real shape) recognizes "win32"
// and "x64" whole despite sitting between two different delimiters — "-" on
// one side, "_" on the other — that no single fixed split could isolate
// together; "x86_64-codex" and "x86_64_codex" both recognize "x86_64" whole
// regardless of which delimiter borders it, for the same reason.
func looseTokenize(name, pkgName string) []string {
	pkgTokens := Tokenize(pkgName)
	bounds := tokenBoundaries(name)
	var out []string
	pos := 0
	for pos < len(name) {
		next := -1
		for _, b := range slices.Backward(bounds) {
			if b <= pos {
				break
			}
			cand := name[pos:b]
			if pkgTokenMatches(cand, pkgTokens) || isStrictVersionToken(cand) || isKnownAliasToken(cand) {
				next = b
				break
			}
		}
		if next == -1 {
			next = len(name)
			for _, b := range bounds {
				if b > pos {
					next = b
					break
				}
			}
		}
		out = append(out, name[pos:next])
		pos = next
		if pos < len(name) && isTokenDelim(name[pos]) {
			pos++
		}
	}
	return out
}

// nonHostSum sums every score in scores except hostKey's own — used to rank
// the "long list" by how many non-host platforms an asset claims.
func nonHostSum(scores map[string]int, hostKey string) int {
	sum := 0
	for key, s := range scores {
		if key != hostKey {
			sum += s
		}
	}
	return sum
}

type scoreResult struct {
	hasNeg     bool
	nameMatch  bool
	osMatch    bool
	archMatch  bool
	osWeight   int // tie-break only: canonical-spelling preference
	archWeight int // tie-break only: canonical-spelling preference
	osWrong    int // tie-break only: how many non-host OSes this claims
	archWrong  int // tie-break only: how many non-host arches this claims
	extra      int // tie-break only: leftover unhelpful-word count
}

// scoreAsset classifies every token of name (see looseTokenize — one shared
// decomposition, not a separate pass per match type) as pkgName, a version,
// a known OS/arch alias, a recognized toolchain hint (gnu/musl/msvc — not
// scored here, secondaryScore weighs those), or leftover/"extra". This one
// shared per-token classification is what drives both the positive matches
// (nameMatch/osMatch/archMatch — booleans, each worth exactly one point) and
// the tie-break signals (osWeight/archWeight/extra) — no separate mechanism
// or double-counting risk between them.
func scoreAsset(name, pkgName string) scoreResult {
	return scoreAssetForHost(name, pkgName, runtime.GOOS, runtime.GOARCH)
}

// scoreAssetForHost is scoreAsset with goos/goarch as explicit parameters —
// split out so the "unlisted host" elimination rule (see below) is testable
// against a platform other than the one actually running the test.
func scoreAssetForHost(name, pkgName, goos, goarch string) scoreResult {
	lower := strings.ToLower(stripAssetExt(name))
	pkg := strings.ToLower(pkgName)
	pkgTokens := Tokenize(pkg)

	var r scoreResult
	osKeys := make(map[string]int)
	archKeys := make(map[string]int)
	extra := make(map[string]bool)

	for _, t := range looseTokenize(lower, pkg) {
		switch {
		case pkg != "" && pkgTokenMatches(t, pkgTokens):
			r.nameMatch = true
		case isStrictVersionToken(t):
		default:
			if key, w, ok := matchAlias(t, osNames); ok {
				if w > osKeys[key] {
					osKeys[key] = w
				}
			} else if key, w, ok := matchAlias(t, archNames); ok {
				if w > archKeys[key] {
					archKeys[key] = w
				}
			} else if _, _, ok := matchAlias(t, toolPrefs); ok {
				// recognized toolchain hint, not a platform id and not extra
			} else {
				extra[t] = true
			}
		}
	}

	r.osWeight = osKeys[goos]
	r.archWeight = archKeys[goarch]
	r.extra = len(extra)

	// Elimination: an unlisted GOOS/GOARCH (no entry in osNames/archNames)
	// matches by default when nothing else claims a specific platform —
	// nothing contradicts it.
	if _, listed := osNames[goos]; listed {
		r.osMatch = osKeys[goos] > 0
	} else {
		r.osMatch = len(osKeys) == 0
	}
	if _, listed := archNames[goarch]; listed {
		r.archMatch = archKeys[goarch] > 0
	} else {
		r.archMatch = len(archKeys) == 0
	}

	r.osWrong = nonHostSum(osKeys, goos)
	r.archWrong = nonHostSum(archKeys, goarch)
	if !r.osMatch && r.osWrong > 0 {
		r.hasNeg = true
	}
	if !r.archMatch && r.archWrong > 0 {
		r.hasNeg = true
	}
	if hasUnknownExt(strings.ToLower(name)) {
		r.hasNeg = true
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

// hasUnknownExt reports a real trailing extension hasRecognizedExt doesn't allow (a digit-led segment is a version fragment, not one).
func hasUnknownExt(lower string) bool {
	i := strings.LastIndexByte(lower, '.')
	if i < 0 || i == len(lower)-1 {
		return false
	}
	if c := lower[i+1]; c < 'a' || c > 'z' {
		return false
	}
	return !hasRecognizedExt(lower)
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
		// No specific release version is being targeted here (hint is a
		// caller-supplied name, not a prior asset from a known release), so
		// only an exact token match can succeed — never a version bump.
		if chosen, ok := matchByHint(candidates, hint, ""); ok {
			return AssetCandidates{Chosen: chosen, All: candidates}, nil
		}
	}

	// candidateScore separates "does this look like the right asset" (active:
	// name/os/arch/ext, each contributing exactly 0 or 1 — flat on purpose, so
	// no dimension can outweigh another) from "which of the tied candidates is
	// the better one" (other, aliasWeight, wrongCount — order/normalize within
	// a tie, never decide which tie an asset is in). Weighting os/arch match
	// strength directly into active (as scoreAsset's own score does) would let
	// a same-repo sibling with a better-spelled arch alias (e.g. "amd64" over
	// "x86_64") outscore the real binary outright — worse than just tying it,
	// since the real binary would then be excluded from the list entirely.
	type candidateScore struct {
		asset       gh.Asset
		active      int
		other       int
		aliasWeight int
		wrongCount  int
		hasNeg      bool
		osMatch     bool
		archMatch   bool
	}
	all := make([]candidateScore, 0, len(candidates))
	for _, a := range candidates {
		lower := strings.ToLower(a.Name)
		sr := scoreAsset(a.Name, pkgName)
		active := 0
		if sr.nameMatch {
			active++
		}
		if sr.osMatch {
			active++
		}
		if sr.archMatch {
			active++
		}
		if hasRecognizedExt(lower) {
			active++
		}
		all = append(all, candidateScore{
			asset: a, active: active, other: sr.extra,
			aliasWeight: sr.osWeight + sr.archWeight,
			wrongCount:  sr.osWrong + sr.archWrong,
			hasNeg:      sr.hasNeg, osMatch: sr.osMatch, archMatch: sr.archMatch,
		})
	}

	// Nothing scored and nothing even claimed a (wrong) platform: report "found nothing" instead of dumping the whole release on the user.
	informative := false
	for _, c := range all {
		if c.active > 0 || c.wrongCount > 0 {
			informative = true
			break
		}
	}
	if len(all) > 0 && !informative {
		return AssetCandidates{}, ErrNoCompatibleAsset
	}

	var compatible, hidden []candidateScore
	for _, c := range all {
		if c.hasNeg {
			hidden = append(hidden, c)
		} else {
			compatible = append(compatible, c)
		}
	}

	// Only the assets tied for the best active score are real contenders — a
	// checksum sidecar or source tarball losing to a confident winner isn't a
	// legitimate alternative choice (unlike a same-repo sibling tool, or a
	// wrong-platform build, both of which land in the long list below), so it
	// drops out entirely rather than cluttering "show more" with packaging
	// noise.
	maxActive := 0
	for _, c := range compatible {
		if c.active > maxActive {
			maxActive = c.active
		}
	}

	var short []candidateScore
	for _, c := range compatible {
		if c.active == maxActive {
			short = append(short, c)
		}
	}

	// Within the tie, order by how clean the mention is (fewer unhelpful
	// words first), then alias-spelling quality (aliasWeight — "amd64" over
	// "x86_64" over "x64" for the same real arch), then the existing
	// tool/extension preference, then name.
	slices.SortStableFunc(short, func(a, b candidateScore) int {
		if d := a.other - b.other; d != 0 {
			return d
		}
		if d := b.aliasWeight - a.aliasWeight; d != 0 {
			return d
		}
		if d := secondaryScore(b.asset.Name) - secondaryScore(a.asset.Name); d != 0 {
			return d
		}
		return strings.Compare(stripAssetExt(a.asset.Name), stripAssetExt(b.asset.Name))
	})

	seen := make(map[string]bool, len(short))
	deduped := short[:0]
	for _, c := range short {
		base := stripAssetExt(c.asset.Name)
		if seen[base] {
			hidden = append(hidden, c)
		} else {
			seen[base] = true
			deduped = append(deduped, c)
		}
	}
	short = deduped

	if len(short) == 1 && short[0].osMatch && short[0].archMatch {
		return AssetCandidates{Chosen: short[0].asset, All: candidates}, nil
	}

	// The long list orders by the same active-score buckets first (so a
	// close-but-not-quite compatible asset outranks one hidden purely for
	// claiming the wrong platform), then by how many wrong-platform hits it
	// racked up, then the same clean-mention/tool/name tie-breaks as short.
	slices.SortStableFunc(hidden, func(a, b candidateScore) int {
		if d := b.active - a.active; d != 0 {
			return d
		}
		if d := a.wrongCount - b.wrongCount; d != 0 {
			return d
		}
		if d := a.other - b.other; d != 0 {
			return d
		}
		if d := b.aliasWeight - a.aliasWeight; d != 0 {
			return d
		}
		if d := secondaryScore(b.asset.Name) - secondaryScore(a.asset.Name); d != 0 {
			return d
		}
		return strings.Compare(stripAssetExt(a.asset.Name), stripAssetExt(b.asset.Name))
	})

	var shortAssets []gh.Asset
	for _, c := range short {
		shortAssets = append(shortAssets, c.asset)
	}
	var hiddenAssets []gh.Asset
	for _, c := range hidden {
		hiddenAssets = append(hiddenAssets, c.asset)
	}

	return AssetCandidates{Compatible: shortAssets, Hidden: hiddenAssets, All: candidates}, nil
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
// selected asset name) in a release normalized to newVersion. Unlike
// SelectAssetAuto it never falls back to platform scoring: a hit means the
// same asset was found in the new release, so callers re-resolving a prior
// selection can distinguish "carried over unchanged" from "had to guess".
// Returns ok=false when the hint matches zero or multiple assets.
func ResolveByHint(assets []gh.Asset, hint, newVersion string) (gh.Asset, bool) {
	return matchByHint(assets, hint, newVersion)
}

func matchByHint(candidates []gh.Asset, hint, newVersion string) (gh.Asset, bool) {
	hintTokens := Tokenize(hint)
	if len(hintTokens) == 0 {
		return gh.Asset{}, false
	}

	var match gh.Asset
	matchCount := 0
	for _, a := range candidates {
		if tokensMatch(hintTokens, Tokenize(a.Name), newVersion) {
			match = a
			matchCount++
		}
	}
	if matchCount == 1 {
		return match, true
	}
	return gh.Asset{}, false
}

// tokensMatch reports whether a and b are the same tokens, order-independent,
// allowing at most one differing pair when that pair is the release's version
// bump (isVersionBump) — e.g. llama.cpp's "b1234" vs "b1245", or a bare
// "0.1.0" vs "0.2.0". Every other token, including the count, must match
// exactly.
func tokensMatch(a, b []string, newVersion string) bool {
	if len(a) != len(b) {
		return false
	}
	leftoverA, leftoverB := multisetDiff(a, b)
	switch len(leftoverA) {
	case 0:
		return true
	case 1:
		return isVersionBump(leftoverA[0], leftoverB[0], newVersion)
	default:
		return false
	}
}

// multisetDiff cancels out tokens common to both a and b (matched by count,
// not position) and returns what's left of each.
func multisetDiff(a, b []string) (leftoverA, leftoverB []string) {
	counts := make(map[string]int, len(a))
	for _, t := range a {
		counts[t]++
	}
	for _, t := range b {
		if counts[t] > 0 {
			counts[t]--
			continue
		}
		leftoverB = append(leftoverB, t)
	}
	for t, n := range counts {
		for range n {
			leftoverA = append(leftoverA, t)
		}
	}
	return leftoverA, leftoverB
}

// isVersionBump reports whether oldTok and newTok are the same version/build
// marker (equal leading and trailing junk per version.SplitJunk — e.g. both
// "b" or both "v"/".zip") with a different embedded version — and that new
// embedded version is actually newVersion, the release being resolved. A
// differing number that *isn't* the release's own version is a packaging
// inconsistency (the asset wasn't bumped to match its release), not a bump
// ghpm should paper over by guessing.
func isVersionBump(oldTok, newTok, newVersion string) bool {
	oldJunkA, oldVer, oldJunkB, ok := version.SplitJunk(oldTok)
	if !ok {
		return false
	}
	newJunkA, newVer, newJunkB, ok := version.SplitJunk(newTok)
	if !ok {
		return false
	}
	if oldJunkA != newJunkA || oldJunkB != newJunkB || oldVer == newVer {
		return false
	}
	return newVer == newVersion
}

func IsVersionToken(t string) bool { return isVersionToken(t) }

func isVersionToken(t string) bool {
	s := t
	if strings.HasPrefix(s, "v") || strings.HasPrefix(s, "V") {
		s = s[1:]
	}
	return len(s) > 0 && s[0] >= '0' && s[0] <= '9'
}
