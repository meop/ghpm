package gh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/ghbin"
	"github.com/meop/golie/vers"
)

func IsRateLimited(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "rate limit")
}

type Client interface {
	GetLatestRelease(ctx context.Context, owner, repo string) (Release, error)
	GetReleaseByTag(ctx context.Context, owner, repo, tag string) (Release, error)
	FindLatestMatching(ctx context.Context, owner, repo string, c config.Constraint) (Release, error)
	ListReleases(ctx context.Context, owner, repo string) ([]Release, error)
	DownloadAsset(ctx context.Context, owner, repo, tag, pattern, dest string) error
	BatchLatestVersions(ctx context.Context, items []BatchItem, cacheTTL string) []BatchResult
}

type CLI struct{}

func NewCLI() *CLI { return &CLI{} }

func (c *CLI) GetLatestRelease(ctx context.Context, owner, repo string) (Release, error) {
	return GetLatestRelease(ctx, owner, repo)
}

func (c *CLI) GetReleaseByTag(ctx context.Context, owner, repo, tag string) (Release, error) {
	return GetReleaseByTag(ctx, owner, repo, tag)
}

func (c *CLI) FindLatestMatching(ctx context.Context, owner, repo string, con config.Constraint) (Release, error) {
	return FindLatestMatching(ctx, owner, repo, con)
}

func (c *CLI) ListReleases(ctx context.Context, owner, repo string) ([]Release, error) {
	return ListReleases(ctx, owner, repo)
}

func (c *CLI) DownloadAsset(ctx context.Context, owner, repo, tag, pattern, dest string) error {
	return DownloadAsset(ctx, owner, repo, tag, pattern, dest)
}

func (c *CLI) BatchLatestVersions(ctx context.Context, items []BatchItem, cacheTTL string) []BatchResult {
	return BatchLatestVersions(ctx, items, cacheTTL)
}

type Asset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	URL    string `json:"url"`
	Digest string `json:"digest"`
}

type Release struct {
	TagName      string  `json:"tagName"`
	IsPrerelease bool    `json:"isPrerelease"`
	Assets       []Asset `json:"assets"`
	// AssetTag is the tag the assets are actually attached to, set only when
	// they came from a pointer hop (see resolvePointerRelease). Empty means
	// they hang off TagName like they normally do.
	AssetTag string `json:"-"`
}

// DownloadTag is the tag to fetch this release's assets from, which is not
// TagName once a pointer hop has moved the assets to another release.
func (r Release) DownloadTag() string {
	if r.AssetTag != "" {
		return r.AssetTag
	}
	return r.TagName
}

// validAssetName reports whether name is a plain filename: no path
// separators, not "." or "..". GitHub's API gives no such guarantee.
func validAssetName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return filepath.Base(name) == name
}

// sanitizeRelease rejects rel if any asset name is unsafe, rather than
// silently dropping just that one asset.
func sanitizeRelease(rel Release) (Release, error) {
	for _, a := range rel.Assets {
		if !validAssetName(a.Name) {
			return Release{}, fmt.Errorf("release %s: unsafe asset name %q", rel.TagName, a.Name)
		}
	}
	return rel, nil
}

// CheckInstalled ensures ghpm has a usable gh, vendoring one if there is
// none yet; fetching is told the archive just before that download.
func CheckInstalled(ctx context.Context, fetching func(archive string)) error {
	if err := ghbin.Ensure(ctx, fetching); err != nil {
		return err
	}
	_, err := ghbin.Find()
	return err
}

func SplitSource(source string) (string, string, error) {
	parts := strings.SplitN(source, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", fmt.Errorf("invalid source %q (want host/owner/repo)", source)
	}
	return parts[1], parts[2], nil
}

func ListReleases(ctx context.Context, owner, repo string) ([]Release, error) {
	return listReleases(ctx, owner, repo, 200)
}

func listReleases(ctx context.Context, owner, repo string, limit int) ([]Release, error) {
	out, err := runCmd(ctx, "gh", "release", "list",
		"-R", owner+"/"+repo,
		"--json", "tagName,isPrerelease",
		"--limit", fmt.Sprint(limit),
	)
	if err != nil {
		return nil, err
	}
	var releases []Release
	if err := json.Unmarshal(out, &releases); err != nil {
		return nil, fmt.Errorf("parsing releases: %w", err)
	}
	return releases, nil
}

func GetLatestRelease(ctx context.Context, owner, repo string) (Release, error) {
	out, err := runCmd(ctx, "gh", "release", "view",
		"-R", owner+"/"+repo,
		"--json", "tagName,assets",
	)
	if err != nil {
		return Release{}, err
	}
	var rel Release
	if err := json.Unmarshal(out, &rel); err != nil {
		return Release{}, fmt.Errorf("parsing release: %w", err)
	}
	rel, err = sanitizeRelease(rel)
	if err != nil {
		return Release{}, err
	}
	rel, err = selectLatestRelease(ctx, owner, repo, rel)
	if err != nil {
		return Release{}, err
	}
	return resolvePointerRelease(ctx, owner, repo, rel)
}

// GetReleaseByTag fetches the release a tag or a version names. A real tag, as sync passes the latest it found, is
// used as given. Otherwise the release is the one whose tag is that version under any prefix a repo tags by ("v1.2",
// "rust-v1.2", "bun-v1.2", "b1234"); where two tags carry the version, as two products of one repo can, the one
// with the prefix of the repo's latest stable release
func GetReleaseByTag(ctx context.Context, owner, repo, tag string) (Release, error) {
	rel, err := getReleaseView(ctx, owner, repo, tag)
	if err == nil {
		return rel, nil
	}
	releases, listErr := listReleases(ctx, owner, repo, releaseLookupLimit)
	if listErr != nil {
		return Release{}, listErr
	}
	match, ok := tagForVersion(releases, tag)
	if !ok {
		return Release{}, err
	}
	return getReleaseView(ctx, owner, repo, match)
}

// releaseLookupLimit is how far back a version is looked for: a repo cutting prereleases daily still reaches years
const releaseLookupLimit = 1000

// tagForVersion is the tag in releases that is ver under any prefix, preferring the prefix the latest stable release
// has
func tagForVersion(releases []Release, ver string) (string, bool) {
	want := vers.Normalize(ver)
	latest := ""
	for _, r := range releases {
		if !r.IsPrerelease {
			latest = tagPrefix(r.TagName)
			break
		}
	}
	var found []string
	for _, r := range releases {
		if vers.Normalize(r.TagName) == want {
			found = append(found, r.TagName)
		}
	}
	for _, t := range found {
		if tagPrefix(t) == latest {
			return t, true
		}
	}
	if len(found) > 0 {
		return found[0], true
	}
	return "", false
}

func FindLatestMatching(ctx context.Context, owner, repo string, c config.Constraint) (Release, error) {
	releases, err := ListReleases(ctx, owner, repo)
	if err != nil {
		return Release{}, err
	}

	bestTag := ""
	for _, r := range releases {
		if r.IsPrerelease || isPrereleaseName(r.TagName) {
			continue
		}
		if !config.Matches(c, r.TagName) {
			continue
		}
		if bestTag == "" || config.CompareVersions(r.TagName, bestTag) > 0 {
			bestTag = r.TagName
		}
	}
	if bestTag == "" {
		return Release{}, fmt.Errorf("no release found matching %q", c.Raw)
	}
	return getReleaseView(ctx, owner, repo, bestTag)
}

func DownloadAsset(ctx context.Context, owner, repo, tag, pattern, dest string) error {
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	_, err := runCmd(ctx, "gh", "release", "download", tag,
		"-R", owner+"/"+repo,
		"-p", pattern,
		"-D", dest,
		"--clobber",
	)
	return err
}

func getReleaseView(ctx context.Context, owner, repo, tag string) (Release, error) {
	rel, err := viewRelease(ctx, owner, repo, tag)
	if err != nil {
		return Release{}, err
	}
	return resolvePointerRelease(ctx, owner, repo, rel)
}

// viewRelease fetches and sanitizes tag's release, with no pointer-hop
// resolution — the fetch a hop itself uses to reach the release it points at,
// so following a pointer doesn't loop back through resolvePointerRelease.
func viewRelease(ctx context.Context, owner, repo, tag string) (Release, error) {
	out, err := runCmd(ctx, "gh", "release", "view", tag,
		"-R", owner+"/"+repo,
		"--json", "tagName,assets",
	)
	if err != nil {
		return Release{}, err
	}
	var rel Release
	if err := json.Unmarshal(out, &rel); err != nil {
		return Release{}, fmt.Errorf("parsing release: %w", err)
	}
	return sanitizeRelease(rel)
}

// runCmd retries exactly once on an auth failure from the vendored gh: it
// re-prompts for a token (ghbin.ReAuth, interactive only) and, if that
// succeeds, runs the exact same command again rather than leaving one dead
// credential to fail every command after it too.
func runCmd(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := runCmdOnce(ctx, name, args...)
	if err == nil || name != "gh" || !errors.Is(err, ErrAuthFailed) {
		return out, err
	}
	if reauthErr := ghbin.ReAuth(ctx); reauthErr != nil {
		return nil, err
	}
	return runCmdOnce(ctx, name, args...)
}

func runCmdOnce(ctx context.Context, name string, args ...string) ([]byte, error) {
	var cmd *exec.Cmd
	if name == "gh" {
		c, err := ghbin.Command(args...)
		if err != nil {
			return nil, err
		}
		cmd = exec.CommandContext(ctx, c.Path, c.Args[1:]...)
		cmd.Env = c.Env
	} else {
		cmd = exec.CommandContext(ctx, name, args...)
	}
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(string(ee.Stderr))
			if strings.Contains(strings.ToLower(stderr), "rate limit") {
				return nil, fmt.Errorf("%w: %s", ErrRateLimited, stderr)
			}
			if isAuthError(stderr) {
				return nil, fmt.Errorf("%w: %s", ErrAuthFailed, stderr)
			}
			return nil, fmt.Errorf("%s: %s", name, stderr)
		}
		return nil, err
	}
	return out, nil
}
