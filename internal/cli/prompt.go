package cli

import "github.com/meop/golie/ui"

// promptConfirm asks msg, or with --yes takes it as answered. Either way the
// work starts a new step after a blank: the confirm line's, or, with no
// confirm line, its own.
func promptConfirm(msg string) bool {
	if yes {
		sep()
		return true
	}
	return ui.Confirm(msg)
}
