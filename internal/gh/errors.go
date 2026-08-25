package gh

import (
	"errors"
	"strings"
)

var ErrRateLimited = errors.New("rate limited")

var ErrAuthFailed = errors.New("gh authentication failed")

// isAuthError reports whether a gh invocation's stderr indicates a bad or
// missing credential — either wording, since ghpm's vendored gh can end up in
// either state (a token that stopped working, or no token stamped at all).
func isAuthError(stderr string) bool {
	lower := strings.ToLower(stderr)
	return strings.Contains(lower, "bad credentials") ||
		strings.Contains(lower, "http 401") ||
		strings.Contains(lower, "gh auth login")
}
