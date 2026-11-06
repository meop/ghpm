// Package toolchain names the tools ghpm vendors for its own use under
// ~/.ghpm/vendor and where their releases come from. Not named "vendor": Go
// reserves that directory name, and a repo that ignores it (as this one does
// at the module root) would silently drop the package.
//
// wedge's release is golie's (golie/wedge), since every tool built on golie
// stamps with it. gh is ghpm's alone, and is described here.
package toolchain

import "github.com/meop/golie/vendored"

// GhReleasesURL is gh's release page. Its latest release is read from where
// the page's "latest" redirects and fetched as a plain download — never the
// GitHub API, so there is no token involved and no rate limit to meet before
// gh exists to carry one. It is a var, not a const, so a test can point it at
// a local server.
var GhReleasesURL = "https://github.com/cli/cli/releases/"

// GhKey is the "<os>_<arch>" gh spells goos/goarch as in its asset names.
func GhKey(goos, goarch string) string {
	if goos == "darwin" {
		goos = "macOS"
	}
	return goos + "_" + goarch
}

// Gh is gh's release as it applies to goos/goarch: its archive there, checked
// against the release's checksums.txt, which gh does not sign. ok is false
// where gh ships none.
func Gh(goos, goarch string) (vendored.Release, bool) {
	if goarch != "amd64" && goarch != "arm64" {
		return vendored.Release{}, false
	}
	ext, bin := ".zip", "gh"
	switch goos {
	case "linux":
		ext = ".tar.gz"
	case "darwin":
	case "windows":
		bin = "gh.exe"
	default:
		return vendored.Release{}, false
	}
	key := GhKey(goos, goarch)
	return vendored.Release{
		Name:        "gh",
		ReleasesURL: GhReleasesURL,
		Archive:     func(version string) (string, bool) { return "gh_" + version + "_" + key + ext, true },
		Sums:        func(version string) string { return "gh_" + version + "_checksums.txt" },
		Files:       []string{bin},
	}, true
}
