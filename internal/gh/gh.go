package gh

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/ghapi"
	"github.com/meop/golie/vers"
)

func IsRateLimited(err error) bool {
	return errors.Is(err, ErrRateLimited) || err != nil && strings.Contains(strings.ToLower(err.Error()), "rate limit")
}

type Client interface {
	GetLatestRelease(ctx context.Context, owner, repo string) (Release, error)
	GetReleaseByTag(ctx context.Context, owner, repo, tag string) (Release, error)
	FindLatestMatching(ctx context.Context, owner, repo string, c config.Constraint) (Release, error)
	ListReleases(ctx context.Context, owner, repo string) ([]Release, error)
	DownloadAsset(ctx context.Context, owner, repo, tag, pattern, dest string) error
	BatchLatestVersions(ctx context.Context, items []BatchItem, cacheTTL string) []BatchResult
}

// CLI is the Client that asks GitHub's API.
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
	ID     int64  `json:"id"`
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
	var releases []Release
	for page := 1; len(releases) < limit; page++ {
		var got []restRelease
		path := fmt.Sprintf("repos/%s/%s/releases?per_page=%d&page=%d", owner, repo, perPage, page)
		if err := ghapi.GetJSON(ctx, path, &got); err != nil {
			return nil, err
		}
		for _, r := range got {
			if r.Draft {
				continue
			}
			releases = append(releases, Release{TagName: r.TagName, IsPrerelease: r.Prerelease})
		}
		if len(got) < perPage {
			break
		}
	}
	if len(releases) > limit {
		releases = releases[:limit]
	}
	return releases, nil
}

// perPage is the most releases GitHub lists in one page
const perPage = 100

// restRelease is a release as GitHub's REST API spells it
type restRelease struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
	Assets     []struct {
		ID                 int64  `json:"id"`
		Name               string `json:"name"`
		Size               int64  `json:"size"`
		Digest             string `json:"digest"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// fetchRelease is the release at path (latest, or tags/<tag>), sanitized
func fetchRelease(ctx context.Context, owner, repo, which string) (Release, error) {
	var r restRelease
	if err := ghapi.GetJSON(ctx, fmt.Sprintf("repos/%s/%s/releases/%s", owner, repo, which), &r); err != nil {
		return Release{}, err
	}
	rel := Release{TagName: r.TagName, IsPrerelease: r.Prerelease}
	for _, a := range r.Assets {
		rel.Assets = append(rel.Assets, Asset{ID: a.ID, Name: a.Name, Size: a.Size, URL: a.BrowserDownloadURL, Digest: a.Digest})
	}
	return sanitizeRelease(rel)
}

func GetLatestRelease(ctx context.Context, owner, repo string) (Release, error) {
	rel, err := fetchRelease(ctx, owner, repo, "latest")
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

// DownloadAsset writes every asset of tag's release whose name matches the
// glob pattern into dest, each replacing what is there by rename. A pattern no
// asset matches is an error.
func DownloadAsset(ctx context.Context, owner, repo, tag, pattern, dest string) error {
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	rel, err := viewRelease(ctx, owner, repo, tag)
	if err != nil {
		return err
	}
	matched := 0
	for _, a := range rel.Assets {
		if ok, _ := path.Match(pattern, a.Name); !ok {
			continue
		}
		matched++
		if err := downloadTo(ctx, fmt.Sprintf("repos/%s/%s/releases/assets/%d", owner, repo, a.ID), filepath.Join(dest, a.Name)); err != nil {
			return fmt.Errorf("%s: %w", a.Name, err)
		}
	}
	if matched == 0 {
		return fmt.Errorf("release %s: no asset matches %q", tag, pattern)
	}
	return nil
}

func downloadTo(ctx context.Context, apiPath, file string) error {
	tmp, err := os.CreateTemp(filepath.Dir(file), "."+filepath.Base(file)+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := ghapi.Download(ctx, apiPath, tmp); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// CreateTemp makes it its owner's alone; a download is anyone's to read
	if err := os.Chmod(tmp.Name(), 0644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), file)
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
	return fetchRelease(ctx, owner, repo, "tags/"+url.PathEscape(tag))
}
