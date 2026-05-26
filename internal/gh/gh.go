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
)

func IsRateLimited(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "rate limit")
}

type Asset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

type Release struct {
	TagName      string  `json:"tagName"`
	IsPrerelease bool    `json:"isPrerelease"`
	Assets       []Asset `json:"assets"`
}

func ghBin() (string, error) {
	if p, err := exec.LookPath("gh"); err == nil {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	managed := home + "/.ghpm/bin/gh"
	if _, err := os.Stat(managed); err == nil {
		return managed, nil
	}
	return "", fmt.Errorf("gh CLI not found — install it from https://cli.github.com/")
}

func CheckInstalled() error {
	_, err := ghBin()
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
		return getReleaseView(ctx, owner, repo, alt)
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

func VerifyAsset(ctx context.Context, owner, repo, tag, cacheDir, assetName string) (bool, error) {
	assetPath := filepath.Join(cacheDir, assetName)
	if _, err := os.Stat(assetPath); os.IsNotExist(err) {
		return false, fmt.Errorf("asset not found: %s", assetPath)
	}

	ghPath, err := ghBin()
	if err != nil {
		return false, err
	}
	cmd := exec.CommandContext(ctx, ghPath, "release", "verify-asset", tag, assetPath, "-R", owner+"/"+repo)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "no attestation") || strings.Contains(string(out), "not found") {
			return false, nil
		}
		return false, fmt.Errorf("verification failed: %s", string(out))
	}
	return true, nil
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
		if p, err := ghBin(); err == nil {
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
