package gh

import "testing"

func TestIsAuthError(t *testing.T) {
	cases := []struct {
		stderr string
		want   bool
	}{
		{"gh: Bad credentials (HTTP 401)", true},
		{`{"message":"Bad credentials","status":"401"}`, true},
		{"gh: To get started with GitHub CLI, please run:  gh auth login", true},
		{"API rate limit exceeded", false},
		{"repository not found", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isAuthError(c.stderr); got != c.want {
			t.Errorf("isAuthError(%q) = %v, want %v", c.stderr, got, c.want)
		}
	}
}
