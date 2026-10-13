package config

import "github.com/meop/golie/vers"

// Version pins and comparison are golie's, shared with every tool built on
// it; these names are how the rest of ghpm has always spelled them.

type (
	PinLevel   = vers.PinLevel
	Constraint = vers.Constraint
)

const (
	PinMajor = vers.PinMajor
	PinMinor = vers.PinMinor
	PinExact = vers.PinExact
)

// ParseConstraint parses a version string into a Constraint. Any leading junk
// is stripped, so a full literal release tag works as a pin input too.
func ParseConstraint(s string) (Constraint, error) { return vers.ParseConstraint(s) }

// CompareVersions returns 1 if a > b, -1 if a < b, 0 if equal.
func CompareVersions(a, b string) int { return vers.CompareVersions(a, b) }

// NormalizeVersion strips all leading non-digit characters from a version string.
func NormalizeVersion(v string) string { return vers.Normalize(v) }
