package config

import (
	"testing"
)

func TestParseConstraint(t *testing.T) {
	cases := []struct {
		input string
		major int
		minor int
		level PinLevel
		raw   string
		err   bool
	}{
		{"14", 14, -1, PinMajor, "14", false},
		{"v14", 14, -1, PinMajor, "14", false},
		{"14.1", 14, 1, PinMinor, "14.1", false},
		{"14.1.0", 14, 1, PinExact, "14.1.0", false},
		{"14.1.2", 14, 1, PinExact, "14.1.2", false},
		{"0.56.0", 0, 56, PinExact, "0.56.0", false},
		{"v0.56.0", 0, 56, PinExact, "0.56.0", false},
		{"notaversion", 0, 0, 0, "", true},
	}
	for _, c := range cases {
		got, err := ParseConstraint(c.input)
		if c.err {
			if err == nil {
				t.Errorf("ParseConstraint(%q) expected error", c.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseConstraint(%q) unexpected error: %v", c.input, err)
			continue
		}
		if got.Major != c.major || got.Minor != c.minor || got.Level != c.level || got.Raw != c.raw {
			t.Errorf("ParseConstraint(%q) = {%d,%d,%d,%q}, want {%d,%d,%d,%q}",
				c.input, got.Major, got.Minor, int(got.Level), got.Raw,
				c.major, c.minor, int(c.level), c.raw)
		}
	}
}

func TestConstraintMatches(t *testing.T) {
	cases := []struct {
		constraint string
		version    string
		want       bool
	}{
		// Major pin
		{"14", "14.1.0", true},
		{"14", "v14.1.0", true},
		{"14", "14.2.3", true},
		{"14", "15.0.0", false},
		{"14", "13.9.9", false},
		// Minor pin
		{"14.1", "14.1.0", true},
		{"14.1", "14.1.9", true},
		{"14.1", "v14.1.5", true},
		{"14.1", "14.2.0", false},
		{"14.1", "15.1.0", false},
		// Exact pin
		{"14.1.0", "14.1.0", true},
		{"14.1.0", "v14.1.0", true},
		{"14.1.0", "14.1.1", false},
		{"14.1.0", "14.2.0", false},
	}
	for _, c := range cases {
		con, err := ParseConstraint(c.constraint)
		if err != nil {
			t.Fatalf("ParseConstraint(%q): %v", c.constraint, err)
		}
		got := con.Matches(c.version)
		if got != c.want {
			t.Errorf("Constraint(%q).Matches(%q) = %v, want %v", c.constraint, c.version, got, c.want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.10.0", "1.9.0", 1},
		{"1.9.0", "1.10.0", -1},
		{"2.0.0", "1.9.9", 1},
		{"1.0.0", "1.0.0", 0},
		{"v1.2.3", "1.2.3", 0},
		{"v1.2.4", "v1.2.3", 1},
		{"14.1.0", "14.0.9", 1},
	}
	for _, c := range cases {
		got := CompareVersions(c.a, c.b)
		if got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestNormalizeVersion(t *testing.T) {
	cases := [][2]string{
		{"v1.2.3", "1.2.3"},
		{"1.2.3", "1.2.3"},
		{"v0.56.0", "0.56.0"},
		{"bun-v1.3.13", "1.3.13"},
		{"bun-v5.6.3", "5.6.3"},
		{"cli-v2.67.0", "2.67.0"},
		{"release-0.1.0", "0.1.0"},
		{"0.0.1", "0.0.1"},
		{"v0.0.1", "0.0.1"},
		{"abc", "abc"},
		{"", ""},
	}
	for _, c := range cases {
		if got := NormalizeVersion(c[0]); got != c[1] {
			t.Errorf("NormalizeVersion(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}
