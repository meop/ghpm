package cli

import "github.com/meop/golie/ui"

func promptConfirm(msg string) bool {
	if yes {
		return true
	}
	return ui.Confirm(msg)
}
