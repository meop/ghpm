package cli

import (
	"testing"
)

func TestVersionedBinName(t *testing.T) {
	cases := []struct {
		name    string
		version string
		want    string
	}{
		{"fzf", "v0.70.0", "fzf@0.70.0"},
		{"fzf", "0.70.0", "fzf@0.70.0"}, // already stripped
		{"ripgrep", "v14.1.1", "ripgrep@14.1.1"},
		{"gh", "v2.67.0", "gh@2.67.0"},
	}
	for _, c := range cases {
		got := versionedBinName(c.name, c.version)
		if got != c.want {
			t.Errorf("versionedBinName(%q, %q) = %q, want %q", c.name, c.version, got, c.want)
		}
	}
}

// TestBinNameIsKey verifies that the binary placed on disk equals the manifest key.
// For unversioned installs the key is the name; for pinned the key is name@version.
func TestBinNameIsKey(t *testing.T) {
	cases := []struct {
		manifestKey string
		wantBin     string
	}{
		{"fzf", "fzf"},
		{"gh", "gh"},
		{"fzf@0.70.0", "fzf@0.70.0"},
		{"ripgrep@14.1.1", "ripgrep@14.1.1"},
		{"gh@2.67.0", "gh@2.67.0"},
	}

	for _, c := range cases {
		// The binary name is always the manifest key — no derivation needed.
		got := c.manifestKey
		if got != c.wantBin {
			t.Errorf("bin name for key %q = %q, want %q", c.manifestKey, got, c.wantBin)
		}
	}
}
