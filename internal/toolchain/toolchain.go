// Package toolchain names the tools ghpm vendors for its own use under
// ~/.ghpm/vendor and the exact version it runs each of them at. Not named
// "vendor": Go reserves that directory name, and a repo that ignores it (as
// this one does at the module root) would silently drop the package.
package toolchain

import (
	"os/exec"
	"strings"

	"github.com/meop/ghpm/internal/version"
)

// The pinned version of each vendored tool. ghpm syncs its own copy to
// exactly these at runtime — a mismatch in *either* direction is corrected,
// so an upgrade and a downgrade are the same operation — which is why they
// are absent from `ghpm upgrade`: they are ghpm's internals, not components a
// user chose to install, and nobody running `ghpm upgrade` means "and also
// move gh". Bumping a constant here is the only way either one moves.
const (
	GhVersion     = "2.100.0"
	SheeshVersion = "0.1.1"
)

// Installed runs `<path> --version` and returns the first version token it
// prints, without a leading "v" — or "" when the binary is missing, isn't
// runnable, or prints nothing version-shaped. The empty string is what drives
// a first-time vendor, so "absent" and "unreadable" deliberately collapse
// into the same answer: re-fetch.
func Installed(path string) string {
	out, err := exec.Command(path, "--version").Output() //nolint:gosec
	if err != nil {
		return ""
	}
	for tok := range strings.FieldsSeq(string(out)) {
		if version.IsVersionToken(tok) {
			return strings.TrimPrefix(tok, "v")
		}
	}
	return ""
}
