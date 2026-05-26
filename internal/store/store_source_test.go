package store

import "testing"

func TestSourceFromPath(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"github.com/meop/ghpm/v1.0.0/asset.tar.gz", "github.com/meop/ghpm"},
		{"github.com/cli/cli/v2.67.0/gh.tar.gz", "github.com/cli/cli"},
		{"short", ""},
	}
	for _, c := range cases {
		got := SourceFromPath(c.input)
		if got != c.want {
			t.Errorf("SourceFromPath(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}
