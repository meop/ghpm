// Package toolchain names the tools ghpm vendors for its own use under
// ~/.ghpm/vendor and the exact release it runs each of them at. Not named
// "vendor": Go reserves that directory name, and a repo that ignores it (as
// this one does at the module root) would silently drop the package.
//
// sheesh's pin is golie's (golie/sheesh), since every tool built on golie
// stamps with the same one. gh is ghpm's alone, and is pinned here.
package toolchain

import "github.com/meop/golie/pinned"

// GhVersion is the pinned gh release. ghpm syncs its own copy to exactly this
// at runtime, by content — see golie's pinned package — which is why it is
// absent from `ghpm upgrade`: it is ghpm's internals, not a component a user
// chose to install, and nobody running `ghpm upgrade` means "and also move
// gh". Bumping it here, with the digests below, is the only way it moves.
const GhVersion = "2.100.0"

// GhReleaseBase is where gh's releases are downloaded from: a plain release
// download addressed by the pinned version, never "latest" and never the
// GitHub API, so there is no token involved and no API rate limit to meet
// before gh exists to carry one. It is a var, not a const, so a test can
// point it at a local server.
var GhReleaseBase = "https://github.com/cli/cli/releases/download/"

// GhAssets are the pinned release's archives, keyed by the "<os>_<arch>" gh
// spells in their names: each one's sha256, and that of the gh inside it.
// `go run github.com/meop/golie/cmd/pin -url <archive> gh` prints an entry.
var GhAssets = map[string]pinned.Asset{
	"linux_amd64":   {SHA256: "e4d4bb4498e8d007abe545b6568926793ace1b6447da598294a610018cb164be", Files: map[string]string{"gh": "553949e2efa12842771efe6012aa4de21f1d591530ec17fc435f610f10e017ee"}},
	"linux_arm64":   {SHA256: "ea4e7a581a32ccad6cc7923cb1576ac5859ba4b9a16ab22eb8f8a96e78e2e961", Files: map[string]string{"gh": "28a037b967065aa314cb6d539943b55d27ef2f97c523ab2b6023ccf284e1828d"}},
	"macOS_amd64":   {SHA256: "fcd7799e85eb575f3c7d2b1679bfbfedaefa1269d4bc7d096b51e10939b4812b", Files: map[string]string{"gh": "43f9c5f8fb3e6ed097bff7d1b7b86158cddddd4fc6b6704b6e1c3c2b7d35dcbd"}},
	"macOS_arm64":   {SHA256: "45f9a62da2f6e641a7fad57e2ce39656dfd7ef331372d80a2a2aed65abb01642", Files: map[string]string{"gh": "51f1bd7ed1724774d2c1d91fb4efdb676d3eb200ce95c0021c8544fe7435bc13"}},
	"windows_amd64": {SHA256: "227e35230b25db3fa1b997bab7cf4d67df0470a3b75b99e4ee66bce1a7cd4e72", Files: map[string]string{"gh.exe": "2ae2b350c227a618f2d8965b1900aeee13446ff42e17ef0bd5a0b6405c593cfb"}},
	"windows_arm64": {SHA256: "7beaeb4743cf255809a8e574a2724c685b566545e04eabea284fe38a56c15b02", Files: map[string]string{"gh.exe": "d36ec6d8429409592dc3a58f169c2c38d986af6f232edf8ddebb6020dfb1dab1"}},
}

// GhKey is the "<os>_<arch>" gh spells goos/goarch as in its asset names.
func GhKey(goos, goarch string) string {
	if goos == "darwin" {
		goos = "macOS"
	}
	return goos + "_" + goarch
}

// Gh is the pinned gh release as it applies to goos/goarch; ok is false where
// gh ships none.
func Gh(goos, goarch string) (pinned.Tool, bool) {
	key := GhKey(goos, goarch)
	asset, ok := GhAssets[key]
	if !ok {
		return pinned.Tool{}, false
	}
	ext := ".zip"
	if goos == "linux" {
		ext = ".tar.gz"
	}
	asset.URL = GhReleaseBase + "v" + GhVersion + "/gh_" + GhVersion + "_" + key + ext
	return pinned.Tool{Name: "gh", Version: GhVersion, Asset: asset}, true
}
