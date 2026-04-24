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
		Parallelism: 5,
		PlatformPriority: config.PlatformPriority{
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
		{"fzf-0.56.0-linux_amd64.tar.gz.sha256", true},
		{"fzf-0.56.0-linux_amd64.tar.gz.sig", true},
		{"fzf-0.56.0.deb", true},
		{"fzf-source.tar.gz", true},
		{"fzf-src.tar.gz", true},
		{"fzf.rpm", true},
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

func TestSelectAsset_PlatformPriorityLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only test")
	}
	// GNU should be preferred over musl on linux
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
