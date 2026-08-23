package gh

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/ghbin"
	"github.com/meop/ghpm/internal/version"
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

func CheckInstalled() error {
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
	out, err := runCmd(ctx, "gh", "release", "list",
		"-R", owner+"/"+repo,
		"--json", "tagName,isPrerelease",
		"--limit", "200",
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
	return resolvePointerRelease(ctx, owner, repo, rel)
}

// GetReleaseByTag fetches the release at tag, trying it exactly as given
// first (the fast path for a caller re-fetching a tag it already confirmed
// exists, e.g. sync refreshing a known release), then falling back through
// candidateTags built from tag's bare version. That covers pinning by just a
// version number regardless of which of ghpm's known tagging conventions the
// repo actually uses.
func GetReleaseByTag(ctx context.Context, owner, repo, tag string) (Release, error) {
	rel, err := getReleaseView(ctx, owner, repo, tag)
	if err == nil {
		return rel, nil
	}
	lastErr := err
	for _, cand := range candidateTags(repo, tag) {
		if cand == tag {
			continue
		}
		rel, err := getReleaseView(ctx, owner, repo, cand)
		if err == nil {
			return rel, nil
		}
		lastErr = err
	}
	return Release{}, lastErr
}

// candidateTags returns tag string variants to try, in priority order: bare
// version, "v"+version, "<repo>-v"+version, "b"+version, "<repo>-b"+version.
// This is every gh release tagging convention ghpm has seen so far: bare or
// "v"-prefixed (most projects), llama.cpp's bare "b"-prefixed build numbers,
// and bun's "<repo>-v"-prefixed tags.
func candidateTags(repo, ver string) []string {
	core := version.Normalize(ver)
	return []string{
		core,
		"v" + core,
		repo + "-v" + core,
		"b" + core,
		repo + "-b" + core,
	}
}

func FindLatestMatching(ctx context.Context, owner, repo string, c config.Constraint) (Release, error) {
	releases, err := ListReleases(ctx, owner, repo)
	if err != nil {
		return Release{}, err
	}

	bestTag := ""
	for _, r := range releases {
		if r.IsPrerelease {
			continue
		}
		if !c.Matches(r.TagName) {
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

func runCmd(ctx context.Context, name string, args ...string) ([]byte, error) {
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
			return nil, fmt.Errorf("%s: %s", name, stderr)
		}
		return nil, err
	}
	return out, nil
}
