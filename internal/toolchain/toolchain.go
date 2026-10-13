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

// Both tools are fetched as plain release downloads — a known URL per
// version and platform, no GitHub API call and no gh — so nothing GitHub
// reports stands between the pin and the bytes. These are the sha256 of each
// asset of the pinned releases, keyed by the "<os>_<arch>" (gh) or
// "<os>-<arch>" (sheesh) each project spells in its asset names. A fetched
// archive is checked against them before it is unpacked, so a pin names
// exact contents, not just a tag; bumping a version above means replacing
// its digests here in the same change.
var (
	GhDigests = map[string]string{
		"linux_amd64":   "e4d4bb4498e8d007abe545b6568926793ace1b6447da598294a610018cb164be",
		"linux_arm64":   "ea4e7a581a32ccad6cc7923cb1576ac5859ba4b9a16ab22eb8f8a96e78e2e961",
		"macOS_amd64":   "fcd7799e85eb575f3c7d2b1679bfbfedaefa1269d4bc7d096b51e10939b4812b",
		"macOS_arm64":   "45f9a62da2f6e641a7fad57e2ce39656dfd7ef331372d80a2a2aed65abb01642",
		"windows_amd64": "227e35230b25db3fa1b997bab7cf4d67df0470a3b75b99e4ee66bce1a7cd4e72",
		"windows_arm64": "7beaeb4743cf255809a8e574a2724c685b566545e04eabea284fe38a56c15b02",
	}
	SheeshDigests = map[string]string{
		"darwin-aarch64":  "3c697949bcdaa32707adb06e803a3875f41b61e191e0e8beba7a4e62c204b87b",
		"darwin-x86_64":   "8d5f0d09974f2208195c26bd8960ef0c10bc8207f617bea3a95ccb2cf98ce8bc",
		"linux-aarch64":   "1552842af9e910f56fa45c7b6197086b784c3db1826cc3e8b4e1648d3ce885e9",
		"linux-x86_64":    "c1a08126ebb3117dd0034ec1ced849c6ef6df3a214f7b4884f07f34ad754de37",
		"windows-aarch64": "9f6ffa11fce29e3ce5a2d796123af7af003a7aad22d8ec97a14263c811d6cd66",
		"windows-x86_64":  "d3192a5661090a28278a784f0988df8eaef19c5be96f761b8fe8a7b26f4a3155",
	}
)

// GhAsset names the pinned gh release's archive for goos/goarch and the
// sha256 pinned for it; ok is false where gh ships none.
func GhAsset(goos, goarch string) (name, sha256 string, ok bool) {
	osName, ext := goos, ".zip"
	switch goos {
	case "linux":
		ext = ".tar.gz"
	case "darwin":
		osName = "macOS"
	}
	key := osName + "_" + goarch
	sha256, ok = GhDigests[key]
	return "gh_" + GhVersion + "_" + key + ext, sha256, ok
}

// SheeshAsset names the pinned sheesh release's archive for goos/goarch and
// the sha256 pinned for it; ok is false where sheesh ships none.
func SheeshAsset(goos, goarch string) (name, sha256 string, ok bool) {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[goarch]
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	key := goos + "-" + arch
	sha256, ok = SheeshDigests[key]
	return "sheesh-" + SheeshVersion + "-" + key + ext, sha256, ok
}

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
