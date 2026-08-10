package version

import "strings"

// SplitJunk splits s into the leading junk (everything before the first
// digit), the version (the first maximal run of digits and "." starting at
// that digit, trimmed back to end on a digit rather than a trailing "."),
// and the trailing junk (everything after). This is the one pattern every gh
// release tagging style seen so far reduces to: "b1234" (leading junk "b"),
// "v1.2.3.4" (leading junk "v"), "bun-v1.2.3.4" (leading junk "bun-v"). The
// scan stops at the first character that's neither a digit nor "." — it
// deliberately does not hunt for a *later* digit run elsewhere in s (e.g. an
// architecture suffix like "x64"), since that would no longer be the same
// version marker. ok is false when s has no digit at all, in which case the
// other return values are empty.
func SplitJunk(s string) (leadingJunk, ver, trailingJunk string, ok bool) {
	first := strings.IndexFunc(s, isDigit)
	if first < 0 {
		return "", "", "", false
	}
	end, lastDigit := first, first
	for end < len(s) && (isDigit(rune(s[end])) || s[end] == '.') {
		if isDigit(rune(s[end])) {
			lastDigit = end
		}
		end++
	}
	return s[:first], s[first : lastDigit+1], s[lastDigit+1:], true
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

// Normalize strips all leading non-digit characters from a version string.
// "bun-v1.3.13" → "1.3.13", "v0.71.0" → "0.71.0", "1.2.3" → "1.2.3".
func Normalize(v string) string {
	_, ver, trailing, ok := SplitJunk(v)
	if !ok {
		return v
	}
	return ver + trailing
}
