package config

import (
	"testing"
)

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

func TestResolveSource_ManifestAndTools(t *testing.T) {
	m := &Manifest{
		Tools: map[string]string{
			"bat": "github.com/sharkdp/bat",
		},
		Installs: map[string]PackageEntry{},
	}
	tools := map[string]string{
		"fzf": "github.com/junegunn/fzf",
		"rg":  "github.com/BurntSushi/ripgrep",
	}

	cases := []struct {
		name    string
		version string
		want    string
		wantErr bool
	}{
		// From manifest sources
		{"bat", "", "github.com/sharkdp/bat", false},
		// From tools
		{"fzf", "", "github.com/junegunn/fzf", false},
		{"rg", "", "github.com/BurntSushi/ripgrep", false},
		// Unknown — falls through to gh search which fails (no gh in test env)
		{"unknowntool", "", "", true},
	}

	for _, c := range cases {
		got, err := ResolveSource(c.name, c.version, m, tools)
		if c.wantErr {
			if err == nil {
				t.Errorf("ResolveSource(%q) expected error, got %q", c.name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ResolveSource(%q) unexpected error: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("ResolveSource(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestFindBySource(t *testing.T) {
	m := &Manifest{
		Tools: map[string]string{
			"gh":  "github.com/cli/cli",
			"fzf": "github.com/junegunn/fzf",
		},
		Installs: map[string]PackageEntry{},
	}

	key, found := FindBySource("github.com/cli/cli", m)
	if !found || key != "gh" {
		t.Errorf("FindBySource(cli/cli) = (%q, %v), want (\"gh\", true)", key, found)
	}

	_, found = FindBySource("github.com/nobody/nothere", m)
	if found {
		t.Error("FindBySource(nothere) should not be found")
	}
}
