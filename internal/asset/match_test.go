package asset

import (
	"reflect"
	"runtime"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
)

func testCfg() *config.Settings {
	return &config.Settings{
		NumParallel: 5,
		PlatPriority: config.PlatPriority{
			"linux":   {"gnu", "musl"},
			"windows": {"msvc", "gnu"},
		},
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
	chosen, err := SelectAsset(assets, testCfg(), "fzf-0.56.0-linux_amd64.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if chosen.Name != "fzf-0.56.0-linux_amd64.tar.gz" {
		t.Errorf("unexpected choice: %s", chosen.Name)
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
	chosen, err := SelectAsset(assets, testCfg(), "")
	if err != nil {
		t.Fatal(err)
	}
	if chosen.Name != "tool-linux-amd64.tar.gz" {
		t.Errorf("unexpected choice: %s", chosen.Name)
	}
}

func TestTokenize(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"claude-win32_x64.zip", []string{"claude", "win32", "x64.zip"}},
		{"tool-unknown-linux-gnu-x86_64.tar.gz", []string{"tool", "unknown", "linux", "gnu", "x86", "64.tar.gz"}},
		{"MyTool Darwin ARM64", []string{"mytool", "darwin", "arm64"}},
	}
	for _, c := range cases {
		got := tokenize(c.input)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("tokenize(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestHasTokenPrefix(t *testing.T) {
	cases := []struct {
		name     string
		prefixes []string
		want     bool
	}{
		// darwin must NOT match "win" prefix
		{"tool-darwin-amd64.tar.gz", []string{"win"}, false},
		// darwin matches "dar"
		{"tool-darwin-amd64.tar.gz", []string{"dar", "mac"}, true},
		// windows matches "win"
		{"tool-windows-x64.zip", []string{"win"}, true},
		// linux matches "lin"
		{"tool-linux-amd64.tar.gz", []string{"lin"}, true},
		// macos matches "mac"
		{"tool-macos-arm64.tar.gz", []string{"dar", "mac"}, true},
		// win32 token matches "win"
		{"claude-win32_x64.zip", []string{"win"}, true},
		// x86_64 — "x86" token matches "x86"
		{"tool-unknown-linux-gnu-x86_64.tar.gz", []string{"x64", "x86", "amd"}, true},
		// aarch64 matches "aarch"
		{"tool-linux-aarch64.tar.gz", []string{"arm", "aarch"}, true},
	}
	for _, c := range cases {
		got := hasTokenPrefix(c.name, c.prefixes)
		if got != c.want {
			t.Errorf("hasTokenPrefix(%q, %v) = %v, want %v", c.name, c.prefixes, got, c.want)
		}
	}
}

func TestSelectAsset_PlatPriorityLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only test")
	}
	assets := []gh.Asset{
		{Name: "tool-unknown-linux-musl-x86_64.tar.gz", Size: 100},
		{Name: "tool-unknown-linux-gnu-x86_64.tar.gz", Size: 100},
	}
	chosen, err := SelectAsset(assets, testCfg(), "")
	if err != nil {
		t.Fatal(err)
	}
	if chosen.Name != "tool-unknown-linux-gnu-x86_64.tar.gz" {
		t.Errorf("expected gnu, got %s", chosen.Name)
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
		{"fzf-0.56.0-linux_amd64.tar.gz", []string{"fzf", "linux", "amd64.tar.gz"}},
		{"fzf-0.71.0-linux_amd64.tar.gz", []string{"fzf", "linux", "amd64.tar.gz"}},
		{"bun-v1.3.13-linux-x64.zip", []string{"bun", "linux", "x64.zip"}},
		{"bun-v1.3.14-linux-x64.zip", []string{"bun", "linux", "x64.zip"}},
		{"ghpm-0.1.7-darwin-amd64-something.tar.gz", []string{"ghpm", "darwin", "amd64", "something.tar.gz"}},
	}
	for _, c := range cases {
		tokens := tokenize(c.input)
		got := stripVersionTokens(tokens)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("stripVersionTokens(tokenize(%q)) = %v, want %v", c.input, got, c.want)
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
