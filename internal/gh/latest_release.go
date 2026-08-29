package gh

import (
	"context"
	"regexp"

	"github.com/meop/ghpm/internal/config"
)

// Release tags can share a repo while describing distinct products: dua-cli
// has v<semver> beside dua-core-v<semver>, and bun has bun-v<semver>. Prefer
// the conventional v<semver> stream, then the name-v<semver> convention, then
// llama.cpp-style b<number> releases. An unrecognised repository retains
// GitHub's default release instead.
var (
	vSemverTag      = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
	prefixedVSemver = regexp.MustCompile(`^.+-v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
	buildNumberTag  = regexp.MustCompile(`^b[0-9]+$`)
)

func releaseStreamRank(rel Release) int {
	if rel.IsPrerelease {
		return 4
	}
	switch {
	case vSemverTag.MatchString(rel.TagName):
		return 0
	case prefixedVSemver.MatchString(rel.TagName):
		return 1
	case buildNumberTag.MatchString(rel.TagName):
		return 2
	default:
		return 3
	}
}

// selectLatestRelease applies the stream precedence above. The selected tag is
// fetched again to retain the full asset data that gh release list omits, via
// viewRelease rather than getReleaseView so that GetLatestRelease stays the one
// place a latest-release lookup resolves a pointer.
func selectLatestRelease(ctx context.Context, owner, repo string, current Release) (Release, error) {
	// A normal v<semver> release is already our highest-priority stream. For
	// unknown styles, retain GitHub's choice instead of adding a list call just
	// to speculate. Only lower-priority recognised streams need disambiguation.
	if rank := releaseStreamRank(current); rank != 1 && rank != 2 {
		return current, nil
	}

	releases, err := ListReleases(ctx, owner, repo)
	if err != nil {
		return Release{}, err
	}
	best := ""
	bestRank := 3
	for _, rel := range releases {
		rank := releaseStreamRank(rel)
		if rank > 2 {
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
