package asset

import (
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
