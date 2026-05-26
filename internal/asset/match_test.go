package asset

import (
	"os"
	"reflect"
	"runtime"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
)

func testCfg() *config.Settings {
	return &config.Settings{
		NumParallel: 5,
	}
}

func TestIsSkipped(t *testing.T) {
	cases := []struct {
		name    string
		skipped bool
	}{
		{"fzf-0.56.0-linux_amd64.tar.gz", false},
		{"tool-0.1.0-darwin-arm64.tgz", false},
		{"tool-0.1.0-windows-amd64.zip", false},
		{"tool-0.1.0-linux-amd64.tar.bz2", false},
		{"tool-0.1.0-linux-amd64.tar.xz", false},
		{"ghpm", true},
		{"bun", true},
		{"fzf-0.56.0-linux_amd64.tar.gz.sha256", true},
		{"fzf-0.56.0-linux_amd64.tar.gz.sha256sum", true},
		{"fzf-0.56.0-linux_amd64.tar.gz.sig", true},
		{"fzf-0.56.0.deb", true},
		{"fzf-source.tar.gz", true},
		{"fzf-src.tar.gz", true},
		{"fzf.rpm", true},
		{"fzf.apk", true},
		{"fzf.msi", true},
		{"fzf.pkg", true},
		{"checksums.txt", true},
	}
	for _, c := range cases {
		got := isSkipped(c.name)
		if got != c.skipped {
			t.Errorf("isSkipped(%q) = %v, want %v", c.name, got, c.skipped)
		}
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

func TestHasTokenPrefix(t *testing.T) {
	cases := []struct {
		name     string
		prefixes []string
		want     bool
	}{
		// darwin must NOT match "windows" prefix
		{"tool-darwin-amd64.tar.gz", []string{"windows"}, false},
		// darwin matches "darwin"
		{"tool-darwin-amd64.tar.gz", []string{"darwin", "macos"}, true},
		// windows matches "windows"
		{"tool-windows-x64.zip", []string{"windows"}, true},
		// linux matches "linux"
		{"tool-linux-amd64.tar.gz", []string{"linux"}, true},
		// lint must NOT match linux prefix (golangci-lint false-positive)
		{"golangci-lint-1.0-darwin-amd64.tar.gz", []string{"linux"}, false},
		// macos matches "macos"
		{"tool-macos-arm64.tar.gz", []string{"darwin", "macos"}, true},
		// osx matches "osx"
		{"tool-osx-arm64.tar.gz", []string{"darwin", "macos", "osx"}, true},
		// win32_x64 does NOT match "windows" (ambiguous short prefix excluded)
		{"claude-win32_x64.zip", []string{"windows"}, false},
		// x86_64 stays whole (dash-only tokenize), matches x86_64 prefix
		{"tool-unknown-linux-gnu-x86_64.tar.gz", []string{"x86_64", "x64", "amd64"}, true},
		// aarch64 matches "aarch64"
		{"tool-linux-aarch64.tar.gz", []string{"arm64", "aarch64"}, true},
	}
	for _, c := range cases {
		got := hasTokenPrefix(c.name, c.prefixes)
		if got != c.want {
			t.Errorf("hasTokenPrefix(%q, %v) = %v, want %v", c.name, c.prefixes, got, c.want)
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

func TestStripVersionTokens(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"ghpm-0.1.6-darwin-amd64.tar.gz", []string{"ghpm", "darwin", "amd64.tar.gz"}},
		{"ghpm-0.1.7-darwin-amd64.tar.gz", []string{"ghpm", "darwin", "amd64.tar.gz"}},
		{"fzf-0.56.0-linux_amd64.tar.gz", []string{"fzf", "linux_amd64.tar.gz"}},
		{"fzf-0.71.0-linux_amd64.tar.gz", []string{"fzf", "linux_amd64.tar.gz"}},
		{"bun-v1.3.13-linux-x64.zip", []string{"bun", "linux", "x64.zip"}},
		{"bun-v1.3.14-linux-x64.zip", []string{"bun", "linux", "x64.zip"}},
		{"ghpm-0.1.7-darwin-amd64-something.tar.gz", []string{"ghpm", "darwin", "amd64", "something.tar.gz"}},
	}
	for _, c := range cases {
		tokens := Tokenize(c.input)
		got := stripVersionTokens(tokens)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("stripVersionTokens(Tokenize(%q)) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestMatchByHint_SameVersion(t *testing.T) {
	candidates := []gh.Asset{
		{Name: "ghpm-0.1.7-darwin-amd64.tar.gz", Size: 100},
		{Name: "ghpm-0.1.7-linux-amd64.tar.gz", Size: 100},
		{Name: "ghpm-0.1.7-windows-amd64.zip", Size: 100},
	}
	chosen, ok := matchByHint(candidates, "ghpm-0.1.6-darwin-amd64.tar.gz")
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
	chosen, ok := matchByHint(candidates, "fzf-0.56.0-linux_amd64.tar.gz")
	if !ok {
		t.Fatal("expected match")
	}
	if chosen.Name != "fzf-0.71.0-linux_amd64.tar.gz" {
		t.Errorf("got %q, want fzf-0.71.0-linux_amd64.tar.gz", chosen.Name)
	}
}

func TestMatchByHint_MultipleMatches(t *testing.T) {
	candidates := []gh.Asset{
		{Name: "tool-0.2.0-linux-amd64.tar.gz", Size: 100},
		{Name: "tool-v0.2.0-linux-amd64.tar.gz", Size: 100},
	}
	_, ok := matchByHint(candidates, "tool-0.1.0-linux-amd64.tar.gz")
	if ok {
		t.Error("expected no unique match when two candidates produce same stripped tokens")
	}
}

func TestMatchByHint_DifferentStructure(t *testing.T) {
	candidates := []gh.Asset{
		{Name: "ghpm-0.1.7-darwin-amd64-something.tar.gz", Size: 100},
	}
	_, ok := matchByHint(candidates, "ghpm-0.1.6-darwin-amd64.tar.gz")
	if ok {
		t.Error("expected no match when structure differs")
	}
}

func TestMatchByHint_BunVPrefix(t *testing.T) {
	candidates := []gh.Asset{
		{Name: "bun-v1.3.14-linux-x64.zip", Size: 100},
		{Name: "bun-v1.3.14-darwin-x64.zip", Size: 100},
	}
	chosen, ok := matchByHint(candidates, "bun-v1.3.13-linux-x64.zip")
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

func TestSelectAssetAuto_MultipleCompatible(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("platform-specific test")
	}
	assets := []gh.Asset{
		{Name: "tool-linux-x64.tar.gz", Size: 100},
		{Name: "tool-linux-amd64.tar.gz", Size: 100},
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
		t.Errorf("expected no auto-select in fallback, got %q", ac.Chosen.Name)
	}
	if len(ac.Hidden) != 0 {
		t.Errorf("expected no hidden in fallback, got %d", len(ac.Hidden))
	}
	if len(ac.Compatible) != 2 {
		t.Errorf("expected 2 in fallback compatible, got %d", len(ac.Compatible))
	}
}

func stdinPipe(t *testing.T, input string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old })
	if _, err = w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPromptWithShowMore_Skip(t *testing.T) {
	assets := []gh.Asset{
		{Name: "tool-linux-amd64.tar.gz", Size: 100},
		{Name: "tool-darwin-amd64.tar.gz", Size: 100},
	}
	stdinPipe(t, "0\n")
	_, err := promptWithShowMore(assets, nil)
	if err != ErrSkip {
		t.Errorf("expected ErrSkip, got %v", err)
	}
}

func TestPromptSelect_Skip(t *testing.T) {
	assets := []gh.Asset{
		{Name: "tool-linux-amd64.tar.gz", Size: 100},
		{Name: "tool-darwin-amd64.tar.gz", Size: 100},
	}
	stdinPipe(t, "0\n")
	_, err := PromptSelect("choose:", assets)
	if err != ErrSkip {
		t.Errorf("expected ErrSkip, got %v", err)
	}
}

func TestTokensMatch(t *testing.T) {
	cases := []struct {
		a, b []string
		want bool
	}{
		{[]string{"a", "b"}, []string{"a", "b"}, true},
		{[]string{"a", "b"}, []string{"a", "c"}, false},
		{[]string{"a"}, []string{"a", "b"}, false},
		{[]string{}, []string{}, true},
		{nil, nil, true},
		{[]string{"a"}, nil, false},
	}
	for _, c := range cases {
		got := tokensMatch(c.a, c.b)
		if got != c.want {
			t.Errorf("tokensMatch(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
