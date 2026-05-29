package version

import "testing"

func TestNormalize(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"bun-v1.3.13", "1.3.13"},
		{"v0.71.0", "0.71.0"},
		{"1.2.3", "1.2.3"},
		{"", ""},
		{"abc", "abc"},
		{"v1", "1"},
	}
	for _, c := range cases {
		got := Normalize(c.input)
		if got != c.want {
			t.Errorf("Normalize(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}
