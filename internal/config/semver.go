package config

import (
	"strconv"
	"strings"

	"github.com/meop/golie/vers"
)

// Version pins and junk-stripping are golie's, shared with every tool built on it; these names are how the rest of
// ghpm has always spelled them. Ordering is ghpm's own: only ghpm meets pre-release tags, since goup installs go's
// stable releases or exactly the one it is told.

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

// CompareVersions returns 1 if a > b, -1 if a < b, 0 if equal, in semver precedence: leading junk is stripped, the
// numeric parts compare in order (a missing part counts as 0), a release ranks above its pre-releases, and pre-release
// identifiers compare one by one (numbers numerically and below words, words lexically or by a trailing number, a
// shorter list first when the rest is equal). Build metadata after "+" is ignored.
func CompareVersions(a, b string) int {
	an, ap := versionCore(a)
	bn, bp := versionCore(b)
	for i := range max(len(an), len(bn), 3) {
		ai, bi := 0, 0
		if i < len(an) {
			ai = an[i]
		}
		if i < len(bn) {
			bi = bn[i]
		}
		if ai != bi {
			return cmpInt(ai, bi)
		}
	}
	switch {
	case ap == "" && bp == "":
		return 0
	case ap == "":
		return 1
	case bp == "":
		return -1
	}
	return comparePrerelease(ap, bp)
}

// Matches reports whether tag satisfies the pin: its major, and its minor for a minor pin, or the whole version for
// an exact one. tag may carry any leading junk (a "v", a package-name prefix, llama.cpp's "b").
func Matches(c Constraint, tag string) bool {
	parts, _ := versionCore(tag)
	if len(parts) == 0 || parts[0] != c.Major {
		return false
	}
	if c.Level >= PinMinor && (len(parts) < 2 || parts[1] != c.Minor) {
		return false
	}
	if c.Level == PinExact {
		return NormalizeVersion(tag) == c.Raw
	}
	return true
}

// IsPrerelease reports whether a version or tag carries a pre-release part after its numbers: "v1.2.0-rc.1",
// "zcode-cli-v3.18.0-rc.1". Build metadata alone ("1.2.0+build.5") is not one.
func IsPrerelease(v string) bool {
	_, pre := versionCore(v)
	return pre != ""
}

// versionCore splits a version or tag into its numeric parts and its pre-release identifiers, without the build.
func versionCore(v string) ([]int, string) {
	_, ver, trailing, ok := vers.SplitJunk(v)
	if !ok {
		return nil, ""
	}
	parts := make([]int, 0, 4)
	for p := range strings.SplitSeq(ver, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			break
		}
		parts = append(parts, n)
	}
	if i := strings.IndexByte(trailing, '+'); i >= 0 {
		trailing = trailing[:i]
	}
	return parts, strings.TrimLeft(trailing, "-.")
}

func comparePrerelease(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := range min(len(as), len(bs)) {
		an, aerr := strconv.Atoi(as[i])
		bn, berr := strconv.Atoi(bs[i])
		switch {
		case aerr == nil && berr == nil:
			if an != bn {
				return cmpInt(an, bn)
			}
		case aerr == nil:
			return -1
		case berr == nil:
			return 1
		default:
			if c := compareWord(as[i], bs[i]); c != 0 {
				return c
			}
		}
	}
	return cmpInt(len(as), len(bs))
}

// compareWord orders words lexically, except that two with the same letters and a trailing number compare by that
// number ("rc10" after "rc9").
func compareWord(a, b string) int {
	al, bl := strings.TrimRight(a, "0123456789"), strings.TrimRight(b, "0123456789")
	if al == bl && al != a && bl != b {
		an, aerr := strconv.Atoi(a[len(al):])
		bn, berr := strconv.Atoi(b[len(bl):])
		if aerr == nil && berr == nil {
			return cmpInt(an, bn)
		}
	}
	return strings.Compare(a, b)
}

func cmpInt(a, b int) int {
	switch {
	case a > b:
		return 1
	case a < b:
		return -1
	}
	return 0
}

// NormalizeVersion strips all leading non-digit characters from a version string.
func NormalizeVersion(v string) string { return vers.Normalize(v) }
