package gh

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/meop/ghpm/internal/config"
)

type Asset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

type Release struct {
	TagName  string  `json:"tagName"`
	Assets   []Asset `json:"assets"`
	IsLatest bool    `json:"isLatest"`
}

// CheckInstalled verifies gh is available.
func CheckInstalled() error {
	_, err := exec.LookPath("gh")
	if err != nil {
		return fmt.Errorf("gh CLI not found — install it from https://cli.github.com/")
	}
	return nil
}

// SplitSource splits "github.com/owner/repo" → owner, repo.
func SplitSource(source string) (string, string, error) {
	s := strings.TrimPrefix(source, "github.com/")
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid source %q (want github.com/owner/repo)", source)
	}
	return parts[0], parts[1], nil
}

// ListReleases returns all releases for owner/repo (tagName + isLatest only).
func ListReleases(owner, repo string) ([]Release, error) {
	out, err := run("gh", "release", "list",
		"-R", owner+"/"+repo,
		"--json", "tagName",
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

// GetLatestRelease fetches the latest release with full asset info.
func GetLatestRelease(owner, repo string) (Release, error) {
	out, err := run("gh", "release", "view",
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

// GetReleaseByTag fetches a specific release by tag with full asset info.
// It tries the tag as given, then the alternate v-prefix form if the first fails.
// e.g. "14.1.0" → tries "14.1.0" then "v14.1.0".
func GetReleaseByTag(owner, repo, tag string) (Release, error) {
	rel, err := getReleaseView(owner, repo, tag)
	if err != nil {
		alt := alternateVTag(tag)
		return getReleaseView(owner, repo, alt)
	}
	return rel, nil
}

// FindLatestMatching lists all releases and returns the full release data for
// the highest-versioned one that satisfies the given constraint.
func FindLatestMatching(owner, repo string, c config.Constraint) (Release, error) {
	releases, err := ListReleases(owner, repo)
	if err != nil {
		return Release{}, err
	}

	bestTag := ""
	for _, r := range releases {
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
	return getReleaseView(owner, repo, bestTag)
}

// DownloadAsset downloads a release asset to dest directory.
func DownloadAsset(owner, repo, tag, pattern, dest string) error {
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	_, err := run("gh", "release", "download", tag,
		"-R", owner+"/"+repo,
		"-p", pattern,
		"-D", dest,
		"--clobber",
	)
	return err
}

func getReleaseView(owner, repo, tag string) (Release, error) {
	out, err := run("gh", "release", "view", tag,
		"-R", owner+"/"+repo,
		"--json", "tagName,assets,isLatest",
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

func run(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(string(ee.Stderr))
			return nil, fmt.Errorf("%s: %s", name, stderr)
		}
		return nil, err
	}
	return out, nil
}
