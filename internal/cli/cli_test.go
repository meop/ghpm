package cli

import (
	"testing"

	"github.com/meop/ghpm/internal/config"
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

// TestUninstallBinNameDerivation verifies that the binary filename derived for
// uninstall matches what install puts on disk.
func TestUninstallBinNameDerivation(t *testing.T) {
	cases := []struct {
		manifestKey string // e.g. "fzf" or "fzf@v0.70.0"
		storedVer   string // pkg.Version as stored in manifest
		versioned   bool
		wantBin     string
	}{
		// Unversioned: binary name == manifest key
		{"fzf", "v0.56.0", false, "fzf"},
		{"gh", "v2.67.0", false, "gh"},
		// Versioned: binary is name@version (v stripped)
		{"fzf@v0.70.0", "v0.70.0", true, "fzf@0.70.0"},
		{"ripgrep@v14.1.1", "v14.1.1", true, "ripgrep@14.1.1"},
		// Version without v prefix already (unusual but safe)
		{"gh@2.67.0", "2.67.0", true, "gh@2.67.0"},
	}

	for _, c := range cases {
		pkg := config.PackageEntry{Version: c.storedVer, Versioned: c.versioned}
		var got string
		if pkg.Versioned {
			name, version, _ := config.ParseVersionSuffix(c.manifestKey)
			got = versionedBinName(name, version)
		} else {
			got = c.manifestKey
		}
		if got != c.wantBin {
			t.Errorf("bin name for key %q = %q, want %q", c.manifestKey, got, c.wantBin)
		}
	}
}
