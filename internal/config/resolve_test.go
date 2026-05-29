package config

import (
	"bufio"
	"os"
	"testing"

	"github.com/meop/ghpm/internal/ioutils"
)

func setStdin(t *testing.T, input string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := ioutils.Stdin
	ioutils.Stdin = bufio.NewReader(r)
	t.Cleanup(func() { ioutils.Stdin = old })
	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
}

func TestParseVersionSuffix(t *testing.T) {
	cases := []struct {
		input   string
		name    string
		version string
		pinned  bool
	}{
		{"fzf", "fzf", "", false},
		{"fzf@0.70", "fzf", "0.70", true},
		{"fzf@v0.70.0", "fzf", "v0.70.0", true},
	}
	for _, c := range cases {
		name, ver, pinned := ParseVersionSuffix(c.input)
		if name != c.name || ver != c.version || pinned != c.pinned {
			t.Errorf("ParseVersionSuffix(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.input, name, ver, pinned, c.name, c.version, c.pinned)
		}
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{"fzf", "gh", "ripgrep", "my-tool", "tool123"}
	invalid := []string{"cli/cli", "github.com/cli/cli", "a b", "", "owner/repo"}

	for _, n := range valid {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q) unexpected error: %v", n, err)
		}
	}
	for _, n := range invalid {
		if err := ValidateName(n); err == nil {
			t.Errorf("ValidateName(%q) expected error, got nil", n)
		}
	}
}

func TestResolveSource_ManifestAndRepos(t *testing.T) {
	m := &Manifest{
		Repos: map[string]string{
			"bat": "github.com/sharkdp/bat",
		},
		Extracts: map[string]PackageEntry{},
	}
	repos := map[string]string{
		"fzf": "github.com/junegunn/fzf",
		"rg":  "github.com/BurntSushi/ripgrep",
	}

	cases := []struct {
		name    string
		version string
		want    string
		wantErr bool
	}{
		// From manifest repos
		{"bat", "", "github.com/sharkdp/bat", false},
		// From repos
		{"fzf", "", "github.com/junegunn/fzf", false},
		{"rg", "", "github.com/BurntSushi/ripgrep", false},
		// Unknown — falls through to gh search which fails (no gh in test env)
		{"unknowntool", "", "", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.wantErr {
				setStdin(t, "0\n")
			}
			got, err := ResolveSource(c.name, c.version, m, repos)
			if c.wantErr {
				if err == nil {
					t.Errorf("ResolveSource(%q) expected error, got %q", c.name, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ResolveSource(%q) unexpected error: %v", c.name, err)
				return
			}
			if got != c.want {
				t.Errorf("ResolveSource(%q) = %q, want %q", c.name, got, c.want)
			}
		})
	}
}

func TestFindBySource(t *testing.T) {
	m := &Manifest{
		Repos: map[string]string{
			"gh":  "github.com/cli/cli",
			"fzf": "github.com/junegunn/fzf",
		},
		Extracts: map[string]PackageEntry{},
	}

	key, found := m.FindBySource("github.com/cli/cli")
	if !found || key != "gh" {
		t.Errorf("FindBySource(cli/cli) = (%q, %v), want (\"gh\", true)", key, found)
	}

	_, found = m.FindBySource("github.com/nobody/nothere")
	if found {
		t.Error("FindBySource(nothere) should not be found")
	}
}
