package config

import "strings"

// The two questions a typed term can put to a name, the same in every command
// (and the same two wut asks, so a name behaves alike in both): a command that
// acts asks whether this is the name, and a command that reads asks whether it
// starts one of the name's words.

// SameName is an acting command's question: add, remove, sync and info take a
// name as typed, never part of one. Case aside, nothing is normalised, so two
// spellings of a name are two names — a registry that wants both states both.
func SameName(name, term string) bool {
	return term != "" && strings.EqualFold(name, term)
}

// WordStartsWith is a reading command's question: what was typed starts a word
// of the name, a word being what sits between anything that is not a letter or
// a digit. "go" finds "golang", "go-task" and "owner/golang", and never
// "cargo". The term is taken as typed, its own separators included, so
// "lm-studio" finds "lm-studio" and "studio" does not find "lmstudio".
func WordStartsWith(name, term string) bool {
	n, t := strings.ToLower(name), strings.ToLower(term)
	if t == "" {
		return false
	}
	for from := 0; ; {
		i := strings.Index(n[from:], t)
		if i < 0 {
			return false
		}
		at := from + i
		if at == 0 || !isWordByte(n[at-1]) {
			return true
		}
		from = at + 1
	}
}

func isWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9'
}
