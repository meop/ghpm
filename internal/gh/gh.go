package gh

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/ghbin"
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

func BinPath() (string, error) { return ghbin.Find() }

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
	return rel, nil
}

func GetReleaseByTag(ctx context.Context, owner, repo, tag string) (Release, error) {
	rel, err := getReleaseView(ctx, owner, repo, tag)
	if err != nil {
		alt := alternateVTag(tag)
		rel2, err2 := getReleaseView(ctx, owner, repo, alt)
		if err2 != nil {
			return Release{}, fmt.Errorf("%v; %v", err, err2)
		}
		return rel2, nil
	}
	return rel, nil
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
	return rel, nil
}

func alternateVTag(tag string) string {
	if strings.HasPrefix(tag, "v") {
		return tag[1:]
	}
	return "v" + tag
}

func runCmd(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == "gh" {
		if p, err := ghbin.Find(); err == nil {
			name = p
		}
	}
	cmd := exec.CommandContext(ctx, name, args...)
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
