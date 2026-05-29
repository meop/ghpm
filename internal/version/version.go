package version

// Normalize strips all leading non-digit characters from a version string.
// "bun-v1.3.13" → "1.3.13", "v0.71.0" → "0.71.0", "1.2.3" → "1.2.3".
func Normalize(v string) string {
	for i, r := range v {
		if r >= '0' && r <= '9' {
			return v[i:]
		}
	}
	return v
}
