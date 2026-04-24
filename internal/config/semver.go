package config

import (
	"fmt"
	"strconv"
	"strings"
)

type PinLevel int

const (
	PinMajor PinLevel = 1 // "14"     — updates within same major
	PinMinor PinLevel = 2 // "14.1"   — updates within same major.minor
	PinExact PinLevel = 3 // "14.1.0" — static, no updates
)

// Constraint represents a parsed version pin from user input.
type Constraint struct {
	Major int
	Minor int      // -1 if not specified
	Level PinLevel // 1, 2, or 3
	Raw   string   // normalised string, v stripped (e.g. "14", "14.1", "14.1.0")
}

func (l PinLevel) String() string {
	switch l {
	case PinMajor:
		return "major"
	case PinMinor:
		return "minor"
	case PinExact:
		return "fixed"
	}
	return "latest"
}

// ParseConstraint parses a version string into a Constraint.
// The v prefix is optional and stripped.
func ParseConstraint(s string) (Constraint, error) {
	s = strings.TrimPrefix(s, "v")
	parts := strings.Split(s, ".")
	c := Constraint{Raw: s, Minor: -1}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return c, fmt.Errorf("invalid version %q: expected numeric major version", s)
	}
	c.Major = major
	c.Level = PinMajor

	if len(parts) >= 2 {
		minor, err := strconv.Atoi(parts[1])
		if err != nil {
			return c, fmt.Errorf("invalid version %q: expected numeric minor version", s)
		}
		c.Minor = minor
		c.Level = PinMinor
	}

	if len(parts) >= 3 {
		c.Level = PinExact
	}

	return c, nil
}

// Matches returns true if the given version satisfies this constraint.
// version may have a v prefix; it is stripped before comparison.
func (c Constraint) Matches(version string) bool {
	parts := versionParts(version)
	if len(parts) == 0 {
		return false
	}
	if parts[0] != c.Major {
		return false
	}
	if c.Level >= PinMinor {
		if len(parts) < 2 || parts[1] != c.Minor {
			return false
		}
	}
	if c.Level == PinExact {
		return strings.TrimPrefix(version, "v") == c.Raw
	}
	return true
}

// CompareVersions returns 1 if a > b, -1 if a < b, 0 if equal.
// Versions are compared numerically part by part; v prefix is stripped.
func CompareVersions(a, b string) int {
	ap := versionParts(a)
	bp := versionParts(b)
	for i := 0; i < 3; i++ {
		ai, bi := 0, 0
		if i < len(ap) {
			ai = ap[i]
		}
		if i < len(bp) {
			bi = bp[i]
		}
		if ai > bi {
			return 1
		}
		if ai < bi {
			return -1
		}
	}
	return 0
}

func versionParts(v string) []int {
	v = strings.TrimPrefix(v, "v")
	raw := strings.Split(v, ".")
	parts := make([]int, 0, len(raw))
	for _, p := range raw {
		n, err := strconv.Atoi(p)
		if err != nil {
			break
		}
		parts = append(parts, n)
	}
	return parts
}

// NormalizeVersion strips the v prefix from a version string.
func NormalizeVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}
