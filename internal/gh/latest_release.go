package gh

import (
	"context"
	"regexp"
	"strings"

	"github.com/meop/ghpm/internal/config"
)

// Release tags can share a repo while describing distinct products: dua-cli
// has v<semver> beside dua-core-v<semver>, and bun has bun-v<semver>. A fork
// keeps its upstream's v<semver> tags beside its own <repo>-v<semver> releases.
// Prefer tags named for the repo itself, then the conventional v<semver>
// stream, then another name-v<semver> stream, then llama.cpp-style b<number>
// releases. An unrecognised repository retains GitHub's default release instead.
var (
	vSemverTag      = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
	prefixedVSemver = regexp.MustCompile(`^.+-v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
	buildNumberTag  = regexp.MustCompile(`^b[0-9]+$`)
)

const (
	rankRepoNamed = iota
	rankVSemver
	rankPrefixed
	rankBuildNumber
	rankUnknown
	rankPrerelease
)

// isRepoNamedTag reports whether tag is <repo>-v<semver>, the repo's own stream.
func isRepoNamedTag(tag, repo string) bool {
	prefix := repo + "-"
	return repo != "" && len(tag) > len(prefix) && strings.EqualFold(tag[:len(prefix)], prefix) &&
		vSemverTag.MatchString(tag[len(prefix):])
}

func releaseStreamRank(rel Release, repo string) int {
	if rel.IsPrerelease {
		return rankPrerelease
	}
	switch {
	case isRepoNamedTag(rel.TagName, repo):
		return rankRepoNamed
	case vSemverTag.MatchString(rel.TagName):
		return rankVSemver
	case prefixedVSemver.MatchString(rel.TagName):
		return rankPrefixed
	case buildNumberTag.MatchString(rel.TagName):
		return rankBuildNumber
	default:
		return rankUnknown
	}
}

// selectLatestRelease applies the stream precedence above. The selected tag is
// fetched again to retain the full asset data that gh release list omits, via
// viewRelease rather than getReleaseView so that GetLatestRelease stays the one
// place a latest-release lookup resolves a pointer.
func selectLatestRelease(ctx context.Context, owner, repo string, current Release) (Release, error) {
	// GitHub's latest is kept when it is repo-named or a plain v<semver>, so a
	// conventional repo costs no list call (a fork has no copy of its upstream's
	// releases, so a v<semver> latest there is its own), and when its style is
	// unknown. A lower recognised stream may have a better release beside it.
	if rank := releaseStreamRank(current, repo); rank <= rankVSemver || rank >= rankUnknown {
		return current, nil
	}

	releases, err := ListReleases(ctx, owner, repo)
	if err != nil {
		return Release{}, err
	}
	best := ""
	bestRank := rankUnknown
	for _, rel := range releases {
		rank := releaseStreamRank(rel, repo)
		if rank >= rankUnknown {
			continue
		}
		if rank < bestRank || (rank == bestRank && config.CompareVersions(rel.TagName, best) > 0) {
			best = rel.TagName
			bestRank = rank
		}
	}
	if best == "" || best == current.TagName {
		return current, nil
	}
	return viewRelease(ctx, owner, repo, best)
}
