package cli

import (
	"testing"

	"github.com/meop/ghpm/internal/asset"
)

func TestParseSourceArg(t *testing.T) {
	cases := []struct {
		input      string
		wantSource string
		wantRepo   string
		wantErr    bool
	}{
		// plain names — not a source form
		{"fzf", "", "", false},
		{"ripgrep", "", "", false},
		// org/repo shorthand → github.com implied (no dot in first segment)
		{"junegunn/fzf", "github.com/junegunn/fzf", "fzf", false},
		{"cli/cli", "github.com/cli/cli", "cli", false},
		// full github.com/org/repo
		{"github.com/junegunn/fzf", "github.com/junegunn/fzf", "fzf", false},
		{"github.com/cli/cli", "github.com/cli/cli", "cli", false},
		// non-GitHub host (dot in first segment → host is explicit)
		{"ghe.example.com/myorg/mytool", "ghe.example.com/myorg/mytool", "mytool", false},
		{"someotherdomain.something.githubv2.com/org/repo", "someotherdomain.something.githubv2.com/org/repo", "repo", false},
		// invalid
		{"github.com/onlyone", "", "", true},
		{"noowner/", "", "", true},
		{"/noleadingslash", "", "", true},
	}
	for _, c := range cases {
		src, repo, err := parseSourceArg(c.input)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseSourceArg(%q) expected error", c.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSourceArg(%q) unexpected error: %v", c.input, err)
			continue
		}
		if src != c.wantSource || repo != c.wantRepo {
			t.Errorf("parseSourceArg(%q) = (%q, %q), want (%q, %q)", c.input, src, repo, c.wantSource, c.wantRepo)
		}
	}
}

func TestBinShimName(t *testing.T) {
	cases := []struct {
		key     string
		binName string
		want    string
	}{
		// unpinned: default shim name == bin name as-is
		{"fzf", "fzf", "fzf"},
		{"codex", "codex-x86_64-unknown-linux-musl", "codex-x86_64-unknown-linux-musl"},
		// pinned: version appended
		{"fzf@0.70.0", "fzf", "fzf@0.70.0"},
		{"codex@0.133.0", "codex-x86_64-unknown-linux-musl", "codex-x86_64-unknown-linux-musl@0.133.0"},
	}
	for _, c := range cases {
		got := deriveShimName(c.key, c.binName)
		if got != c.want {
			t.Errorf("deriveShimName(%q, %q) = %q, want %q", c.key, c.binName, got, c.want)
		}
	}
}

func TestHasReservedConflict(t *testing.T) {
	reserved := map[string]string{"codex": "openai-codex", "bat": "sharkdp-bat"}
	if !hasReservedConflict([]string{"codex"}, reserved) {
		t.Error("expected conflict for codex")
	}
	if hasReservedConflict([]string{"fzf"}, reserved) {
		t.Error("expected no conflict for fzf")
	}
	if !hasReservedConflict([]string{"fzf", "bat"}, reserved) {
		t.Error("expected conflict when bat is in the list")
	}
}

func TestNeedsShimRenamePrompt(t *testing.T) {
	cases := []struct {
		pkgName  string
		selected []asset.BinCandidate
		want     bool
	}{
		{"fzf", []asset.BinCandidate{{BinName: "fzf"}}, false},
		{"bat", []asset.BinCandidate{{BinName: "bat"}}, false},
		{"codex", []asset.BinCandidate{{BinName: "codex-x86_64-unknown-linux-musl"}}, true},
		{"ripgrep", []asset.BinCandidate{{BinName: "rg"}}, true},
		{"uv", []asset.BinCandidate{{BinName: "uv"}, {BinName: "uvx"}}, true},
	}
	for _, c := range cases {
		got := needsShimRenamePrompt(c.pkgName, c.selected)
		if got != c.want {
			t.Errorf("needsShimRenamePrompt(%q, ...) = %v, want %v", c.pkgName, got, c.want)
		}
	}
}

func TestProposedShimNames(t *testing.T) {
	cases := []struct {
		manifestKey string
		selected    []asset.BinCandidate
		want        []string
	}{
		{"codex", []asset.BinCandidate{{BinName: "codex-x86_64-unknown-linux-musl"}},
			[]string{"codex-x86_64-unknown-linux-musl"}},
		{"uv", []asset.BinCandidate{{BinName: "uv"}, {BinName: "uvx"}},
			[]string{"uv", "uvx"}},
		// duplicate filenames get disambiguated by last BinDir segment
		{"foo", []asset.BinCandidate{
			{BinDir: "tools/v1", BinName: "foo"},
			{BinDir: "tools/v2", BinName: "foo"},
		}, []string{"foo-v1", "foo-v2"}},
		// duplicate with no BinDir falls back to index
		{"foo", []asset.BinCandidate{
			{BinDir: "", BinName: "foo"},
			{BinDir: "", BinName: "foo"},
		}, []string{"foo-1", "foo-2"}},
	}
	for _, c := range cases {
		got := proposedShimNames(c.manifestKey, c.selected)
		if len(got) != len(c.want) {
			t.Errorf("proposedShimNames(%q, ...) len=%d, want %d", c.manifestKey, len(got), len(c.want))
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("proposedShimNames(%q, ...)[%d] = %q, want %q", c.manifestKey, i, got[i], c.want[i])
			}
		}
	}
}

func TestSplitBinKey(t *testing.T) {
	cases := []struct {
		key      string
		wantDir  string
		wantName string
	}{
		{"fzf", "", "fzf"},
		{"bin/rg", "bin", "rg"},
		{"tools/v1/codex.exe", "tools/v1", "codex.exe"},
	}
	for _, c := range cases {
		gotDir, gotName := parseBinPath(c.key)
		if gotDir != c.wantDir || gotName != c.wantName {
			t.Errorf("parseBinPath(%q) = (%q, %q), want (%q, %q)", c.key, gotDir, gotName, c.wantDir, c.wantName)
		}
	}
}
