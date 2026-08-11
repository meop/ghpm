package asset

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/ui"
)

func testCfg() *config.Settings {
	return &config.Settings{
		NumParallel: 5,
	}
}

func TestHasRecognizedExt(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"fzf-0.56.0-linux_amd64.tar.gz", true},
		{"tool-0.1.0-darwin-arm64.tgz", true},
		{"tool-0.1.0-windows-amd64.zip", true},
		{"tool-0.1.0-linux-amd64.tar.bz2", true},
		{"tool-linux-amd64.gz", true}, // bare compression, no tar
		{"shfmt_v3.13.1_windows_amd64.exe", true},
		// bare names with no extension at all — real shfmt/jq release shape
		{"shfmt_v3.13.1_linux_amd64", false},
		{"jq-linux-amd64", false},
		// nothing ghpm knows how to unpack or run directly
		{"tool-linux-amd64.tar.Z", false},  // legacy Unix compress: not decoded
		{"tool-linux-amd64.tar.lz", false}, // lzip: not decoded
		{"fzf-0.56.0-linux_amd64.tar.gz.sha256", false},
		{"fzf-0.56.0.deb", false},
		{"checksums.txt", false},
		{"LICENSE", false},
		{"README.md", false},
	}
	for _, c := range cases {
		got := hasRecognizedExt(strings.ToLower(c.name))
		if got != c.want {
			t.Errorf("hasRecognizedExt(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestSelectAssetAuto_JunkLosesOnScoreAlone confirms nothing needs to
// pre-filter checksums/docs/source-tarballs by name: a real binary already
// out-scores them (pkgName + OS/arch + recognized-extension match), so they
// never make it into the auto-selected or compatible/best set. A checksum
// sidecar in particular would otherwise *tie* the real binary's score (same
// pkgName, same OS/arch tokens) if hasRecognizedExt didn't exist — sidecars
// don't end in a recognized extension, so they lose that point.
func TestSelectAssetAuto_JunkLosesOnScoreAlone(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("platform-specific test")
	}
	assets := []gh.Asset{
		{Name: "fzf-0.56.0-linux_amd64.tar.gz", Size: 1_000_000},
		{Name: "fzf-0.56.0-linux_amd64.tar.gz.sha256", Size: 89},
		{Name: "fzf-0.56.0-darwin_amd64.tar.gz", Size: 1_000_000},
		{Name: "fzf-source.tar.gz", Size: 500_000},
		{Name: "checksums.txt", Size: 200},
		{Name: "LICENSE", Size: 1_100},
	}
	ac, err := SelectAssetAuto(assets, testCfg(), "", "fzf")
	if err != nil {
		t.Fatal(err)
	}
	if ac.Chosen.Name != "fzf-0.56.0-linux_amd64.tar.gz" {
		t.Errorf("expected auto-select of the real binary, got Chosen=%q Compatible=%v", ac.Chosen.Name, ac.Compatible)
	}
}

func TestSelectAsset_ExactHint(t *testing.T) {
	assets := []gh.Asset{
		{Name: "fzf-0.56.0-linux_amd64.tar.gz", Size: 1000},
		{Name: "fzf-0.56.0-darwin_amd64.tar.gz", Size: 1000},
	}
	ac, err := SelectAssetAuto(assets, testCfg(), "fzf-0.56.0-linux_amd64.tar.gz", "")
	if err != nil {
		t.Fatal(err)
	}
	if ac.Chosen.Name != "fzf-0.56.0-linux_amd64.tar.gz" {
		t.Errorf("unexpected choice: %s", ac.Chosen.Name)
	}
}

func TestSelectAsset_PlatformMatch(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("platform-specific test")
	}
	assets := []gh.Asset{
		{Name: "tool-linux-amd64.tar.gz", Size: 100},
		{Name: "tool-darwin-amd64.tar.gz", Size: 100},
		{Name: "tool-windows-amd64.zip", Size: 100},
		{Name: "tool-linux-amd64.tar.gz.sha256", Size: 10},
	}
	ac, err := SelectAssetAuto(assets, testCfg(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if ac.Chosen.Name != "tool-linux-amd64.tar.gz" {
		t.Errorf("unexpected choice: %s", ac.Chosen.Name)
	}
}

// An unrecognized-OS build (no distro alias) is still offered, just never
// auto-picked, while a "win"-tagged one is excluded outright.
func TestSelectAsset_ExcludesAbbreviatedOtherOS(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("platform-specific test")
	}
	assets := []gh.Asset{
		{Name: "tool-bin-win-cuda-x64.zip", Size: 100},
		{Name: "tool-bin-someunlisteddistro-x64.tar.gz", Size: 100},
	}
	ac, err := SelectAssetAuto(assets, testCfg(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if ac.Chosen.Name != "" {
		t.Fatalf("expected a prompt (no distro alias to auto-pick from), got silent choice %q", ac.Chosen.Name)
	}
	if len(ac.Compatible) != 1 || ac.Compatible[0].Name != "tool-bin-someunlisteddistro-x64.tar.gz" {
		t.Errorf("expected only the non-Windows build offered, got compatible=%v", ac.Compatible)
	}
	if len(ac.Hidden) != 1 || ac.Hidden[0].Name != "tool-bin-win-cuda-x64.zip" {
		t.Errorf("expected the win-tagged build hidden as Windows-only, got hidden=%v", ac.Hidden)
	}
}

func TestTokenize(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"claude-win32_x64.zip", []string{"claude", "win32_x64.zip"}},
		{"tool-unknown-linux-gnu-x86_64.tar.gz", []string{"tool", "unknown", "linux", "gnu", "x86_64.tar.gz"}},
		{"MyTool Darwin ARM64", []string{"mytool", "darwin", "arm64"}},
	}
	for _, c := range cases {
		got := Tokenize(c.input)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Tokenize(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

// TestScoreAsset_PlatformDetection guards the same cases TestContainsAnyOf
// used to cover under the old boundary-substring matcher, now against
// scoreAsset's exact-token-with-digit-trim matcher (see tokenEquals). Several
// of these ("mac" not matching inside "cmake", "win" not matching inside
// "darwin"/"kotlin") are safer under this design by construction — token
// equality never does substring containment at all — but "win32"/"win64"
// matching the "win" alias and "claude-win32_x64.zip" (two different
// delimiters straddling the same token) still need to work, and did, via
// looseTokenize's coarse-then-fallback tiering (see its comment).
func TestScoreAsset_PlatformDetection(t *testing.T) {
	cases := []struct {
		name     string
		wantOS   bool
		wantArch bool
	}{
		{"tool-darwin-amd64.tar.gz", false, true},
		{"tool-macos-arm64.tar.gz", false, false},
		{"tool-osx-arm64.tar.gz", false, false},
		{"tool-mac-arm64.tar.gz", false, false},
		// "mac" must not match inside "cmake".
		{"cmake-tool-linux-amd64.tar.gz", true, true},
		{"tool-windows-x64.zip", false, true},
		{"tool-linux-amd64.tar.gz", true, true},
		{"golangci-lint-1.0-darwin-amd64.tar.gz", false, true},
		{"tool-unknown-linux-gnu-x86_64.tar.gz", true, true},
		{"tool-linux-aarch64.tar.gz", true, false},
		{"bottom_x86_64-pc-windows-msvc.zip", false, true},
		{"bottom_i686-pc-windows-msvc.zip", false, false},
		// "win" must not match inside "darwin".
		{"tool-darwin-arm64.tar.gz", false, false},
		// llama.cpp names its Windows builds "win", not "windows".
		{"llama-b1-bin-win-cuda-12.4-x64.zip", false, true},
		// "win32"/"win64" sanitize to "win" via digit-trim, and survive the
		// mixed "-" then "_" delimiters straddling them.
		{"claude-win32_x64.zip", false, true},
		{"tool-bin-win64-cuda.zip", false, false},
		// "lin" must not match inside "kotlin"/"berlin".
		{"tool-kotlin-plugin-amd64.tar.gz", false, true},
		// llama.cpp names its Linux builds "ubuntu", which ghpm has no alias
		// for — osNames intentionally doesn't enumerate distros.
		{"llama-b1-bin-ubuntu-x64.tar.gz", false, true},
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("platform-specific test")
	}
	for _, c := range cases {
		sr := scoreAsset(c.name, "")
		if sr.osMatch != c.wantOS {
			t.Errorf("scoreAsset(%q).osMatch = %v, want %v", c.name, sr.osMatch, c.wantOS)
		}
		if sr.archMatch != c.wantArch {
			t.Errorf("scoreAsset(%q).archMatch = %v, want %v", c.name, sr.archMatch, c.wantArch)
		}
	}
}

func TestIsVersionToken(t *testing.T) {
	cases := []struct {
		token string
		want  bool
	}{
		{"0.1.6", true},
		{"v1.2.3", true},
		{"V2.0.0", true},
		{"0.56.0", true},
		{"14", true},
		{"v14", true},
		{"bun", false},
		{"linux", false},
		{"amd64.tar.gz", false},
		{"x64.zip", false},
		{"darwin", false},
		{"gnu", false},
		{"tar.gz", false},
		{"sha256", false},
		{"win32", false},
	}
	for _, c := range cases {
		got := isVersionToken(c.token)
		if got != c.want {
			t.Errorf("isVersionToken(%q) = %v, want %v", c.token, got, c.want)
		}
	}
}

func TestMatchByHint_SameVersion(t *testing.T) {
	candidates := []gh.Asset{
		{Name: "ghpm-0.1.7-darwin-amd64.tar.gz", Size: 100},
		{Name: "ghpm-0.1.7-linux-amd64.tar.gz", Size: 100},
		{Name: "ghpm-0.1.7-windows-amd64.zip", Size: 100},
	}
	chosen, ok := matchByHint(candidates, "ghpm-0.1.6-darwin-amd64.tar.gz", "0.1.7")
	if !ok {
		t.Fatal("expected match")
	}
	if chosen.Name != "ghpm-0.1.7-darwin-amd64.tar.gz" {
		t.Errorf("got %q, want ghpm-0.1.7-darwin-amd64.tar.gz", chosen.Name)
	}
}

func TestMatchByHint_CrossVersion(t *testing.T) {
	candidates := []gh.Asset{
		{Name: "fzf-0.71.0-linux_amd64.tar.gz", Size: 100},
		{Name: "fzf-0.71.0-darwin_amd64.tar.gz", Size: 100},
	}
	chosen, ok := matchByHint(candidates, "fzf-0.56.0-linux_amd64.tar.gz", "0.71.0")
	if !ok {
		t.Fatal("expected match")
	}
	if chosen.Name != "fzf-0.71.0-linux_amd64.tar.gz" {
		t.Errorf("got %q, want fzf-0.71.0-linux_amd64.tar.gz", chosen.Name)
	}
}

func TestMatchByHint_MultipleMatches(t *testing.T) {
	// Two differently-cased asset names that tokenize identically (Tokenize
	// lowercases) — both an exact match for the hint, genuinely ambiguous
	// regardless of newVersion (an exact match never even consults it).
	candidates := []gh.Asset{
		{Name: "tool-linux-amd64.tar.gz", Size: 100},
		{Name: "Tool-Linux-AMD64.tar.gz", Size: 100},
	}
	_, ok := matchByHint(candidates, "tool-linux-amd64.tar.gz", "")
	if ok {
		t.Error("expected no unique match when two candidates tokenize identically")
	}
}

// TestMatchByHint_BuildNumberPrefix guards the llama.cpp case: a "b<n>"
// build-number token (not "v"-prefixed, so isVersionToken never recognized
// it) was compared for exact equality and never matched across versions,
// forcing a full asset reprompt on every sync even though nothing about the
// user's prior selection had actually changed.
func TestMatchByHint_BuildNumberPrefix(t *testing.T) {
	candidates := []gh.Asset{
		{Name: "llama-b2345-bin-ubuntu-x64.zip", Size: 100},
		{Name: "llama-b2345-bin-macos-arm64.zip", Size: 100},
	}
	chosen, ok := matchByHint(candidates, "llama-b2300-bin-ubuntu-x64.zip", "2345")
	if !ok {
		t.Fatal("expected the build-number bump to resolve cleanly")
	}
	if chosen.Name != "llama-b2345-bin-ubuntu-x64.zip" {
		t.Errorf("got %q, want llama-b2345-bin-ubuntu-x64.zip", chosen.Name)
	}
}

// TestMatchByHint_VersionMismatch_NoMatch guards a packaging inconsistency:
// the differing token looks like a version bump in shape (same "b" prefix,
// digits differ), but its embedded version isn't the release actually being
// resolved. That must not be silently accepted.
func TestMatchByHint_VersionMismatch_NoMatch(t *testing.T) {
	candidates := []gh.Asset{
		{Name: "llama-b2346-bin-ubuntu-x64.zip", Size: 100},
	}
	_, ok := matchByHint(candidates, "llama-b2300-bin-ubuntu-x64.zip", "2345")
	if ok {
		t.Error("expected no match when the asset's version doesn't match the release's own version")
	}
}

// TestMatchByHint_TokenOrderIndependent guards that a hint still resolves
// when the differing (version-bumped) token isn't the last-differing one
// positionally relative to how the platform tokens happen to be scanned —
// matching is order-independent, not just tolerant of the version position.
func TestMatchByHint_TokenOrderIndependent(t *testing.T) {
	candidates := []gh.Asset{
		{Name: "tool-linux-b200-amd64.tar.gz", Size: 100},
	}
	chosen, ok := matchByHint(candidates, "tool-b100-linux-amd64.tar.gz", "200")
	if !ok {
		t.Fatal("expected match regardless of where the version token sits")
	}
	if chosen.Name != "tool-linux-b200-amd64.tar.gz" {
		t.Errorf("got %q, want tool-linux-b200-amd64.tar.gz", chosen.Name)
	}
}

func TestMatchByHint_DifferentStructure(t *testing.T) {
	candidates := []gh.Asset{
		{Name: "ghpm-0.1.7-darwin-amd64-something.tar.gz", Size: 100},
	}
	_, ok := matchByHint(candidates, "ghpm-0.1.6-darwin-amd64.tar.gz", "0.1.7")
	if ok {
		t.Error("expected no match when structure differs")
	}
}

func TestMatchByHint_BunVPrefix(t *testing.T) {
	candidates := []gh.Asset{
		{Name: "bun-v1.3.14-linux-x64.zip", Size: 100},
		{Name: "bun-v1.3.14-darwin-x64.zip", Size: 100},
	}
	chosen, ok := matchByHint(candidates, "bun-v1.3.13-linux-x64.zip", "1.3.14")
	if !ok {
		t.Fatal("expected match")
	}
	if chosen.Name != "bun-v1.3.14-linux-x64.zip" {
		t.Errorf("got %q, want bun-v1.3.14-linux-x64.zip", chosen.Name)
	}
}

func TestScoreAsset_HasNegative(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("platform-specific test")
	}
	cases := []struct {
		name    string
		wantNeg bool
	}{
		{"tool-linux-amd64.tar.gz", false},
		{"tool-darwin-amd64.tar.gz", true},
		{"tool-osx-amd64.tar.gz", true},
		{"tool-windows-amd64.zip", true},
		{"tool-linux-arm64.tar.gz", true},
		{"tool-generic.tar.gz", false},
	}
	for _, c := range cases {
		got := scoreAsset(c.name, "").hasNeg
		if got != c.wantNeg {
			t.Errorf("scoreAsset(%q) hasNeg = %v, want %v", c.name, got, c.wantNeg)
		}
	}
}

// covers hosts osNames/archNames has no entry for (e.g. freebsd, loong64)
func TestScoreAssetForHost_UnlistedHost(t *testing.T) {
	cases := []struct {
		name     string
		goos     string
		goarch   string
		wantOS   bool
		wantArch bool
		wantNeg  bool
	}{
		// no known-OS token at all: match by elimination
		{"tool-amd64.tar.gz", "freebsd", "amd64", true, true, false},
		// claims a different known OS: excluded, not elimination
		{"tool-linux-amd64.tar.gz", "freebsd", "amd64", false, true, true},
		// no known-arch token at all: match by elimination
		{"tool-linux.tar.gz", "linux", "loong64", true, true, false},
		// claims a different known arch: excluded, not elimination
		{"tool-linux-arm64.tar.gz", "linux", "loong64", true, false, true},
	}
	for _, c := range cases {
		sr := scoreAssetForHost(c.name, "", c.goos, c.goarch)
		if sr.osMatch != c.wantOS || sr.archMatch != c.wantArch || sr.hasNeg != c.wantNeg {
			t.Errorf("scoreAssetForHost(%q, goos=%q, goarch=%q) = (osMatch=%v, archMatch=%v, hasNeg=%v), want (%v, %v, %v)",
				c.name, c.goos, c.goarch, sr.osMatch, sr.archMatch, sr.hasNeg, c.wantOS, c.wantArch, c.wantNeg)
		}
	}
}

func TestSelectAssetAuto_SingleCompatible(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("platform-specific test")
	}
	assets := []gh.Asset{
		{Name: "tool-linux-amd64.tar.gz", Size: 100},
		{Name: "tool-darwin-amd64.tar.gz", Size: 100},
		{Name: "tool-windows-amd64.zip", Size: 100},
	}
	ac, err := SelectAssetAuto(assets, testCfg(), "", "tool")
	if err != nil {
		t.Fatal(err)
	}
	if ac.Chosen.Name != "tool-linux-amd64.tar.gz" {
		t.Errorf("expected auto-select, got Chosen=%q Compatible=%v", ac.Chosen.Name, ac.Compatible)
	}
}

// TestSelectAssetAuto_BareBinaries covers shfmt's real release shape: no
// archives at all, just a bare per-platform binary (with .exe on Windows).
func TestSelectAssetAuto_BareBinaries(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("platform-specific test")
	}
	assets := []gh.Asset{
		{Name: "shfmt_v3.13.1_darwin_amd64", Size: 100},
		{Name: "shfmt_v3.13.1_darwin_arm64", Size: 100},
		{Name: "shfmt_v3.13.1_linux_386", Size: 100},
		{Name: "shfmt_v3.13.1_linux_amd64", Size: 100},
		{Name: "shfmt_v3.13.1_linux_arm", Size: 100},
		{Name: "shfmt_v3.13.1_linux_arm64", Size: 100},
		{Name: "shfmt_v3.13.1_windows_386.exe", Size: 100},
		{Name: "shfmt_v3.13.1_windows_amd64.exe", Size: 100},
	}
	ac, err := SelectAssetAuto(assets, testCfg(), "", "shfmt")
	if err != nil {
		t.Fatal(err)
	}
	if ac.Chosen.Name != "shfmt_v3.13.1_linux_amd64" {
		t.Errorf("expected auto-select, got Chosen=%q Compatible=%v", ac.Chosen.Name, ac.Compatible)
	}
}

// TestSelectAssetAuto_PrefersCanonicalAlias covers osNames/archNames'
// positional weighting: "amd64" (index 0 of archNames["amd64"]) and "x64"
// (index 2) both flatten to the same active score (arch match is a boolean,
// not a magnitude — a same-repo sibling with the "better" spelling must never
// outscore and exclude the real binary), so this is a genuine tie and both
// land in Compatible. aliasWeight only breaks the tie in ordering, listing
// the canonical spelling first, rather than auto-selecting between two real
// candidates the user never got a chance to see.
func TestSelectAssetAuto_PrefersCanonicalAlias(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("platform-specific test")
	}
	assets := []gh.Asset{
		{Name: "tool-linux-x64.tar.gz", Size: 100},
		{Name: "tool-linux-amd64.tar.gz", Size: 100},
	}
	ac, err := SelectAssetAuto(assets, testCfg(), "", "tool")
	if err != nil {
		t.Fatal(err)
	}
	if ac.Chosen.Name != "" {
		t.Errorf("expected a prompt, not an auto-select, got Chosen=%q", ac.Chosen.Name)
	}
	if len(ac.Compatible) != 2 || ac.Compatible[0].Name != "tool-linux-amd64.tar.gz" || ac.Compatible[1].Name != "tool-linux-x64.tar.gz" {
		t.Errorf("expected amd64 ordered first, both compatible, got %v", ac.Compatible)
	}
}

func TestSelectAssetAuto_MultipleCompatible(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("platform-specific test")
	}
	// "-a"/"-b" are genuinely arbitrary — nothing in scoreAsset or
	// secondaryScore weighs them, so these two truly tie and must prompt,
	// unlike a canonical-vs-alias pair (see PrefersCanonicalAlias above).
	assets := []gh.Asset{
		{Name: "tool-linux-amd64-a.tar.gz", Size: 100},
		{Name: "tool-linux-amd64-b.tar.gz", Size: 100},
		{Name: "tool-darwin-amd64.tar.gz", Size: 100},
	}
	ac, err := SelectAssetAuto(assets, testCfg(), "", "tool")
	if err != nil {
		t.Fatal(err)
	}
	if ac.Chosen.Name != "" {
		t.Errorf("expected no auto-select, got %q", ac.Chosen.Name)
	}
	if len(ac.Compatible) != 2 {
		t.Errorf("expected 2 compatible, got %d: %v", len(ac.Compatible), ac.Compatible)
	}
	if len(ac.Hidden) != 1 {
		t.Errorf("expected 1 hidden, got %d: %v", len(ac.Hidden), ac.Hidden)
	}
}

func TestSelectAssetAuto_NoCompatible(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("platform-specific test")
	}
	assets := []gh.Asset{
		{Name: "tool-darwin-amd64.tar.gz", Size: 100},
		{Name: "tool-windows-amd64.zip", Size: 100},
	}
	ac, err := SelectAssetAuto(assets, testCfg(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if ac.Chosen.Name != "" {
		t.Errorf("expected no auto-select, got %q", ac.Chosen.Name)
	}
	if len(ac.Compatible) != 0 {
		t.Errorf("expected 0 compatible, got %d", len(ac.Compatible))
	}
	if len(ac.Hidden) != 2 {
		t.Errorf("expected 2 hidden, got %d", len(ac.Hidden))
	}
}

// TestSelectAssetAuto_NothingScoredReturnsError guards against dumping an
// entire release's asset list (a monorepo release can ship 150+) on the user
// when not one of them scored any signal at all — pkgName didn't match, and
// nothing suggests a platform, archive, or binary either. That's ghpm's
// heuristics coming up empty, not a real choice for the user to make, so it
// reports the same "no compatible assets" error as an empty release instead.
func TestSelectAssetAuto_NothingScoredReturnsError(t *testing.T) {
	assets := []gh.Asset{
		{Name: "readme.txt", Size: 100},
		{Name: "notes.dat", Size: 100},
	}
	_, err := SelectAssetAuto(assets, testCfg(), "", "tool")
	if !errors.Is(err, ErrNoCompatibleAsset) {
		t.Errorf("expected ErrNoCompatibleAsset, got %v", err)
	}
}

func stdinPipe(t *testing.T, input string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	ui.SetInput(r)
	t.Cleanup(func() {
		os.Stdin = oldStdin
		ui.SetInput(oldStdin)
	})
	if _, err = w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPromptWithShowMore_Empty_SelectsFirst(t *testing.T) {
	assets := []gh.Asset{
		{Name: "a.tar.gz", Size: 1},
		{Name: "b.tar.gz", Size: 2},
	}
	stdinPipe(t, "\n")
	got, err := promptWithShowMore(assets, nil, "")
	if err != nil || got.Name != "a.tar.gz" {
		t.Errorf("got %q, %v; want a.tar.gz, nil", got.Name, err)
	}
}

func TestPromptWithShowMore_SelectSecond(t *testing.T) {
	assets := []gh.Asset{
		{Name: "a.tar.gz", Size: 1},
		{Name: "b.tar.gz", Size: 2},
	}
	stdinPipe(t, "2\n")
	got, err := promptWithShowMore(assets, nil, "")
	if err != nil || got.Name != "b.tar.gz" {
		t.Errorf("got %q, %v; want b.tar.gz, nil", got.Name, err)
	}
}

func TestPromptWithShowMore_Skip(t *testing.T) {
	assets := []gh.Asset{
		{Name: "tool-linux-amd64.tar.gz", Size: 100},
		{Name: "tool-darwin-amd64.tar.gz", Size: 100},
	}
	stdinPipe(t, "0\n")
	_, err := promptWithShowMore(assets, nil, "")
	if err != ErrSkip {
		t.Errorf("expected ErrSkip, got %v", err)
	}
}

func TestPromptWithShowMore_ShowMore_Empty_SelectsFirst(t *testing.T) {
	compatible := []gh.Asset{{Name: "a.tar.gz", Size: 1}, {Name: "b.tar.gz", Size: 2}}
	hidden := []gh.Asset{{Name: "c.tar.gz", Size: 3}}
	stdinPipe(t, "3\n\n")
	got, err := promptWithShowMore(compatible, hidden, "")
	if err != nil || got.Name != "a.tar.gz" {
		t.Errorf("got %q, %v; want a.tar.gz, nil", got.Name, err)
	}
}

// TestPromptFromCandidates_Label guards that the single-asset prompt names its
// package when it has no preceding context line (sync/download/upgrade).
func TestPromptFromCandidates_Label(t *testing.T) {
	stdinPipe(t, "\n")
	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })
	ac := AssetCandidates{Compatible: []gh.Asset{
		{Name: "a.tar.gz", Size: 1},
		{Name: "b.tar.gz", Size: 2},
	}}
	got, err := PromptFromCandidates(ac, "yay")
	if err != nil || got.Name != "a.tar.gz" {
		t.Fatalf("got %q, %v; want a.tar.gz, nil", got.Name, err)
	}
	if !strings.Contains(buf.String(), "yay: choose asset:\n") {
		t.Errorf("missing package label in prompt header:\n%q", buf.String())
	}
}

func TestPromptSelect_Empty_SelectsFirst(t *testing.T) {
	assets := []gh.Asset{
		{Name: "a.tar.gz", Size: 1},
		{Name: "b.tar.gz", Size: 2},
	}
	stdinPipe(t, "\n")
	got, err := PromptSelect("choose:", assets, "")
	if err != nil || got.Name != "a.tar.gz" {
		t.Errorf("got %q, %v; want a.tar.gz, nil", got.Name, err)
	}
}

func TestPromptSelect_SelectSecond(t *testing.T) {
	assets := []gh.Asset{
		{Name: "a.tar.gz", Size: 1},
		{Name: "b.tar.gz", Size: 2},
	}
	stdinPipe(t, "2\n")
	got, err := PromptSelect("choose:", assets, "")
	if err != nil || got.Name != "b.tar.gz" {
		t.Errorf("got %q, %v; want b.tar.gz, nil", got.Name, err)
	}
}

func TestPromptSelect_Skip(t *testing.T) {
	assets := []gh.Asset{
		{Name: "tool-linux-amd64.tar.gz", Size: 100},
		{Name: "tool-darwin-amd64.tar.gz", Size: 100},
	}
	stdinPipe(t, "0\n")
	_, err := PromptSelect("choose:", assets, "")
	if err != ErrSkip {
		t.Errorf("expected ErrSkip, got %v", err)
	}
}

func TestPromptAssetsMulti_AutoChosen(t *testing.T) {
	chosen := gh.Asset{Name: "a.tar.gz", Size: 1}
	got, err := PromptAssetsMulti(AssetCandidates{Chosen: chosen}, "")
	if err != nil || len(got) != 1 || got[0].Name != "a.tar.gz" {
		t.Errorf("got %v, %v; want [a.tar.gz], nil", got, err)
	}
}

func TestPromptAssetsMulti_Empty_SelectsFirst(t *testing.T) {
	compatible := []gh.Asset{
		{Name: "a.tar.gz", Size: 1},
		{Name: "b.tar.gz", Size: 2},
	}
	stdinPipe(t, "\n")
	got, err := PromptAssetsMulti(AssetCandidates{Compatible: compatible}, "")
	if err != nil || len(got) != 1 || got[0].Name != "a.tar.gz" {
		t.Errorf("got %v, %v; want [a.tar.gz], nil", got, err)
	}
}

func TestPromptAssetsMulti_SelectMultiple(t *testing.T) {
	compatible := []gh.Asset{
		{Name: "a.tar.gz", Size: 1},
		{Name: "b.tar.gz", Size: 2},
	}
	stdinPipe(t, "1,2\n")
	got, err := PromptAssetsMulti(AssetCandidates{Compatible: compatible}, "")
	if err != nil || len(got) != 2 {
		t.Errorf("got %v, %v; want 2 assets, nil", got, err)
	}
}

func TestPromptAssetsMulti_Skip(t *testing.T) {
	compatible := []gh.Asset{
		{Name: "a.tar.gz", Size: 1},
		{Name: "b.tar.gz", Size: 2},
	}
	stdinPipe(t, "0\n")
	_, err := PromptAssetsMulti(AssetCandidates{Compatible: compatible}, "")
	if err != ErrSkip {
		t.Errorf("expected ErrSkip, got %v", err)
	}
}

func TestPromptAssetsMulti_ShowMore_Empty_SelectsFirst(t *testing.T) {
	compatible := []gh.Asset{{Name: "a.tar.gz", Size: 1}, {Name: "b.tar.gz", Size: 2}}
	hidden := []gh.Asset{{Name: "c.tar.gz", Size: 3}}
	stdinPipe(t, "3\n\n")
	got, err := PromptAssetsMulti(AssetCandidates{Compatible: compatible, Hidden: hidden}, "")
	if err != nil || len(got) != 1 || got[0].Name != "a.tar.gz" {
		t.Errorf("got %v, %v; want [a.tar.gz], nil", got, err)
	}
}

func TestTokensMatch(t *testing.T) {
	cases := []struct {
		a, b       []string
		newVersion string
		want       bool
	}{
		{[]string{"a", "b"}, []string{"a", "b"}, "", true},
		{[]string{"a", "b"}, []string{"a", "c"}, "", false},
		{[]string{"a"}, []string{"a", "b"}, "", false},
		{[]string{}, []string{}, "", true},
		{nil, nil, "", true},
		{[]string{"a"}, nil, "", false},
		// A single differing pair is only forgiven when it looks like a
		// version/build bump to newVersion — "b" vs "c" has no digits at all.
		{[]string{"a", "b1"}, []string{"a", "b2"}, "2", true},
		{[]string{"a", "b1"}, []string{"a", "b2"}, "3", false}, // bumps to some other version
		{[]string{"a", "b1"}, []string{"a", "c1"}, "1", false},
		// Order doesn't matter, only the multiset.
		{[]string{"a", "b", "c1"}, []string{"c2", "b", "a"}, "2", true},
		// More than one token differs — never forgiven, regardless of
		// newVersion, even if each pair individually looks like a bump.
		{[]string{"a", "b1", "c1"}, []string{"a", "b2", "c2"}, "2", false},
	}
	for _, c := range cases {
		got := tokensMatch(c.a, c.b, c.newVersion)
		if got != c.want {
			t.Errorf("tokensMatch(%v, %v, %q) = %v, want %v", c.a, c.b, c.newVersion, got, c.want)
		}
	}
}

func TestIsVersionBump(t *testing.T) {
	cases := []struct {
		old, new, newVersion string
		want                 bool
	}{
		{"b1234", "b1245", "1245", true},
		{"b1234", "b1245", "9999", false}, // bumped, but not to the release's own version
		{"b1234", "b1234", "1234", false}, // identical isn't a "bump"
		{"b1234", "v1245", "1245", false}, // non-digit shape differs
		{"0.1.0", "0.2.0", "0.2.0", true},
		{"v1.3.13", "v1.3.14", "1.3.14", true},
		{"linux", "darwin", "", false}, // no digits at all
		{"b1234", "linux", "", false},
	}
	for _, c := range cases {
		got := isVersionBump(c.old, c.new, c.newVersion)
		if got != c.want {
			t.Errorf("isVersionBump(%q, %q, %q) = %v, want %v", c.old, c.new, c.newVersion, got, c.want)
		}
	}
}

func TestStripAssetExt(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"tool-linux-amd64.tar.gz", "tool-linux-amd64"},
		{"tool-linux-amd64.tgz", "tool-linux-amd64"},
		{"tool-linux-amd64.tar.bz2", "tool-linux-amd64"},
		{"tool-linux-amd64.tar.xz", "tool-linux-amd64"},
		{"tool-linux-amd64.zip", "tool-linux-amd64"},
		{"tool-linux-amd64", "tool-linux-amd64"},
		{"Tool-Linux-AMD64.TAR.GZ", "tool-linux-amd64"},
	}
	for _, c := range cases {
		if got := stripAssetExt(c.name); got != c.want {
			t.Errorf("stripAssetExt(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestIsKnownAliasToken(t *testing.T) {
	cases := []struct {
		token string
		want  bool
	}{
		{"linux", true},
		{"amd64", true},
		{"x86_64", true},
		{"musl", true},
		// win32/win64 sanitize to "win" once digits are stripped — not a
		// substring or boundary check, an exact match against the stripped form.
		{"win32", true},
		{"win64", true},
		{"codex", false},
		{"app", false},
		{"unknown", false},
	}
	for _, c := range cases {
		if got := isKnownAliasToken(c.token); got != c.want {
			t.Errorf("isKnownAliasToken(%q) = %v, want %v", c.token, got, c.want)
		}
	}
}

// TestExtraTokenCount_BoundaryAndDelimiterAgnostic guards the exact cases
// worked through by hand: pkgName and a platform alias should net zero extra
// tokens regardless of which order they appear in or which delimiter
// separates them — including "x86_64_codex", where "_" is the only delimiter
// present at all. looseTokenize's greedy longest-match search always tries
// "x86_64" whole before ever trying to split at its own internal "_", the
// same way it recognizes "bottom_x86_64" (a real asset shape) whole — there's
// no ambiguity to fall back on here, the longer known token always wins.
// "amd64codex" glued with no delimiter at all is genuinely unrecognizable as
// either, since no span of it (long or short) matches anything known.
func TestExtraTokenCount_BoundaryAndDelimiterAgnostic(t *testing.T) {
	cases := []struct {
		name, pkgName string
		want          int
	}{
		{"x86_64-codex", "codex", 0},
		{"codex-x86_64", "codex", 0},
		{"amd64_codex", "codex", 0},
		{"x86_64_codex", "codex", 0},
		{"amd64codex", "codex", 1},
	}
	for _, c := range cases {
		if got := scoreAsset(c.name, c.pkgName).extra; got != c.want {
			t.Errorf("scoreAsset(%q, %q).extra = %d, want %d", c.name, c.pkgName, got, c.want)
		}
	}
}

// TestExtraTokenCount_RepeatsCountOnce guards that a repeated leftover word
// only counts once — same as a repeated pkgName/alias/version mention
// doesn't earn (or cost) extra credit either.
func TestExtraTokenCount_RepeatsCountOnce(t *testing.T) {
	if got := scoreAsset("codex-foo-foo-x86_64-linux", "codex").extra; got != 1 {
		t.Errorf("extra repeated leftover = %d, want 1", got)
	}
}

// TestExtraTokenCount_PeriodDelimiter guards "." as a fallback split
// delimiter alongside "_", for a name that glues pkgName directly to
// something else with a period rather than a dash or underscore.
func TestExtraTokenCount_PeriodDelimiter(t *testing.T) {
	if got := scoreAsset("codex.app-x86_64-linux", "codex").extra; got != 1 {
		t.Errorf("scoreAsset(%q).extra = %d, want 1 (just \"app\")", "codex.app-x86_64-linux", got)
	}
}

// TestSelectAssetAuto_MonorepoSiblingTools is a compact reproduction of the
// reported openai/codex bug: a monorepo release ships several tools under
// one repo, each named "pkgname-<subtool>-<platform>", so a sibling tool's
// asset also contains pkgName as a delimited token and ties the real
// binary's *active* (name+os+arch+ext) score exactly — that tie is real and
// stays a tie (all three remain in Compatible, requiring a choice, rather
// than one being silently auto-installed). What changes is the order:
// "other" (extraTokenCount) breaks the tie by preferring the name with fewer
// non-pkgName, non-platform words, so the real "codex" binary sorts first —
// the correct default — instead of losing an alphabetical coin flip to
// "codex-app-server" (see git history for the real 150+-asset release this
// was diagnosed against).
func TestSelectAssetAuto_MonorepoSiblingTools(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("platform-specific test")
	}
	assets := []gh.Asset{
		{Name: "codex-app-server-x86_64-unknown-linux-musl.tar.gz", Size: 100},
		{Name: "codex-code-mode-host-x86_64-unknown-linux-musl.tar.gz", Size: 100},
		{Name: "codex-x86_64-unknown-linux-musl.tar.gz", Size: 100},
	}
	ac, err := SelectAssetAuto(assets, testCfg(), "", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if ac.Chosen.Name != "" {
		t.Fatalf("expected a genuine tie (no auto-pick), got silent Chosen=%q", ac.Chosen.Name)
	}
	if len(ac.Compatible) != 3 {
		t.Fatalf("expected all 3 tied siblings to stay visible in Compatible, got %d: %v", len(ac.Compatible), ac.Compatible)
	}
	if ac.Compatible[0].Name != "codex-x86_64-unknown-linux-musl.tar.gz" {
		t.Errorf("expected the real codex binary to sort first, got Compatible=%v", ac.Compatible)
	}
}

func TestSelectAssetAuto_Dedup(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("platform-specific test")
	}
	assets := []gh.Asset{
		{Name: "tool-linux-amd64.tar.gz", Size: 100},
		{Name: "tool-linux-amd64.zip", Size: 100},
		{Name: "tool-darwin-amd64.tar.gz", Size: 100},
	}
	ac, err := SelectAssetAuto(assets, testCfg(), "", "tool")
	if err != nil {
		t.Fatal(err)
	}
	if ac.Chosen.Name != "tool-linux-amd64.tar.gz" {
		t.Errorf("expected auto-select of tar.gz, got Chosen=%q Compatible=%v", ac.Chosen.Name, ac.Compatible)
	}
}

func TestSelectAssetAuto_CompatibleAlphabeticalOrder(t *testing.T) {
	assets := []gh.Asset{
		{Name: "tool-zzz.zip", Size: 100},
		{Name: "tool-aaa.zip", Size: 100},
		{Name: "tool-mmm.zip", Size: 100},
	}
	ac, err := SelectAssetAuto(assets, testCfg(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ac.Compatible) != 3 {
		t.Fatalf("expected 3 compatible, got %d", len(ac.Compatible))
	}
	want := []string{"tool-aaa.zip", "tool-mmm.zip", "tool-zzz.zip"}
	for i, a := range ac.Compatible {
		if a.Name != want[i] {
			t.Errorf("Compatible[%d] = %q, want %q", i, a.Name, want[i])
		}
	}
}

func TestSecondaryScore_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-specific test")
	}
	cases := []struct {
		name string
		want int
	}{
		{"tool-linux-gnu-amd64.tar.gz", 17},  // gnu(+2) + tar.gz(+15)
		{"tool-linux-musl-amd64.tar.gz", 16}, // musl(+1) + tar.gz(+15)
		{"tool-linux-amd64.tar.gz", 15},      // tar.gz(+15)
		{"tool-linux-amd64.tgz", 14},         // tgz(+14)
		{"tool-linux-amd64.tar.bz2", 6},      // tar.bz2(+6) — grouped with the bz2 family, near the end
		{"tool-linux-amd64.tar.xz", 12},      // tar.xz(+12)
		{"tool-linux-amd64.gz", 13},          // bare gz(+13) — grouped right after tar.gz/tgz
		{"tool-linux-gnu-amd64.zip", 3},      // gnu(+2) + zip(+1)
		{"tool-linux-amd64.zip", 1},          // zip(+1)
		// "magnum" contains "gnu" as a substring but not as a delimited segment
		// ('a' before, 'm' after both block it) — must not earn the gnu bonus.
		{"tool-magnum-amd64.tar.gz", 15}, // tar.gz(+15) only
	}
	for _, c := range cases {
		if got := secondaryScore(c.name); got != c.want {
			t.Errorf("secondaryScore(%q) = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestSecondaryScore_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific test")
	}
	cases := []struct {
		name string
		want int
	}{
		{"bottom_x86_64-pc-windows-msvc.zip", 17}, // msvc(+2) + zip(+15)
		{"bottom_x86_64-pc-windows-gnu.zip", 16},  // gnu(+1) + zip(+15)
		{"bottom_i686-pc-windows-msvc.zip", 17},   // msvc(+2) + zip(+15)
		// windows' preference is unix's reversed, so tar.gz (unix's most
		// preferred) is windows' *least* preferred — zip/7z are native there.
		// secondaryScore picks by longest matching suffix, not list
		// position, so this still correctly lands on "tar.gz" (+1) rather
		// than the bare "gz" entry that reversal puts earlier in the list.
		{"tool-windows-msvc.tar.gz", 3}, // msvc(+2) + tar.gz(+1)
		{"tool-windows-gnu.tar.gz", 2},  // gnu(+1) + tar.gz(+1)
		{"tool-windows-msvc.tar.xz", 8}, // msvc(+2) + tar.xz(+6)
		{"tool-windows-amd64.zip", 15},  // zip(+15)
	}
	for _, c := range cases {
		if got := secondaryScore(c.name); got != c.want {
			t.Errorf("secondaryScore(%q) = %d, want %d", c.name, got, c.want)
		}
	}
}
