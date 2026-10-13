package config

import "testing"

func TestWordStartsWith(t *testing.T) {
	cases := []struct {
		name, term string
		want       bool
	}{
		// the term starts a word, and a separator is whatever is not a letter or a digit
		{"golang", "go", true},
		{"go-task", "go", true},
		{"owner/golang", "go", true},
		{"github.com/BurntSushi/ripgrep", "rip", true},
		{"github.com/BurntSushi/ripgrep", "burnt", true},
		{"A cat clone with syntax highlighting.", "syntax", true},
		// the middle or the end of a word is not its start
		{"cargo", "go", false},
		{"ripgrep", "grep", false},
		{"lmstudio", "studio", false},
		// the term is taken as typed, case aside, its own separators included
		{"lm-studio", "studio", true},
		{"lm-studio", "LM-Studio", true},
		{"lmstudio", "lm-studio", false},
		{"claude-code", "claudecode", false},
		// a digit is part of a word, and nothing matches an empty term
		{"7zip", "7z", true},
		{"7zip", "zip", false},
		{"python3", "python", true},
		{"anything", "", false},
	}
	for _, c := range cases {
		if got := WordStartsWith(c.name, c.term); got != c.want {
			t.Errorf("WordStartsWith(%q, %q) = %v, want %v", c.name, c.term, got, c.want)
		}
	}
}

func TestSameName(t *testing.T) {
	cases := []struct {
		name, term string
		want       bool
	}{
		{"ripgrep", "RipGrep", true},
		{"ripgrep", "rip", false},
		{"lm-studio", "lmstudio", false},
		{"ripgrep", "", false},
	}
	for _, c := range cases {
		if got := SameName(c.name, c.term); got != c.want {
			t.Errorf("SameName(%q, %q) = %v, want %v", c.name, c.term, got, c.want)
		}
	}
}
