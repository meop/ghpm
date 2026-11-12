package config

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.10.0", "1.9.0", 1},
		{"1.9.0", "1.10.0", -1},
		{"1.0.0", "1.0.0", 0},
		{"v1.2.3", "1.2.3", 0},
		{"14.1.0", "14.0.9", 1},
		// a release ranks above its pre-releases, in semver's own order
		{"1.0.0", "1.0.0-rc.1", 1},
		{"1.0.0-rc.1", "1.0.0-beta.11", 1},
		{"1.0.0-beta.11", "1.0.0-beta.2", 1},
		{"1.0.0-beta.2", "1.0.0-beta", 1},
		{"1.0.0-beta", "1.0.0-alpha.beta", 1},
		{"1.0.0-alpha.beta", "1.0.0-alpha.1", 1},
		{"1.0.0-alpha.1", "1.0.0-alpha", 1},
		{"1.0.0-rc.10", "1.0.0-rc.9", 1},
		{"1.0.0-rc10", "1.0.0-rc9", 1},
		// the patch counts on a pre-release too, and a prefixed tag compares by its version
		{"3.17.2-rc.1", "3.17.1", 1},
		{"zcode-cli-v3.18.0", "zcode-cli-v3.18.0-rc.1", 1},
		{"zcode-cli-v3.18.0-rc.2", "v3.17.2", 1},
		// a fourth part counts, build metadata doesn't
		{"bun-v1.2.3.5", "bun-v1.2.3.4", 1},
		{"1.2.0+build.9", "1.2.0+build.1", 0},
		{"1.2.0", "1.2", 0},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := CompareVersions(c.b, c.a); got != -c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
}

func TestIsPrerelease(t *testing.T) {
	cases := map[string]bool{
		"1.2.0":                  false,
		"v1.2.0":                 false,
		"zcode-cli-v3.17.2":      false,
		"1.2.0+build.5":          false,
		"b1234":                  false,
		"v1.2.0-rc.1":            true,
		"zcode-cli-v3.18.0-rc.1": true,
		"0.1.0-alpha.1":          true,
		"1.2.0-beta.1+build.5":   true,
	}
	for v, want := range cases {
		if got := IsPrerelease(v); got != want {
			t.Errorf("IsPrerelease(%q) = %v, want %v", v, got, want)
		}
	}
}
