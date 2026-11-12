package gh

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/fakegh"
	"github.com/meop/ghpm/internal/ghapi"
	"github.com/meop/golie/ui"
)

// withTestHome isolates ~/.ghpm (and anywhere else HOME-derived) to a temp
// dir, same as the rest of the test suite's withHome helpers.
func withTestHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("GHPM_TEST_HOME", tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	return tmp
}

// fakeAPI stands up a fake GitHub for this test, in its own home.
func fakeAPI(t *testing.T) *fakegh.Server {
	t.Helper()
	withTestHome(t)
	return fakegh.New(t)
}

// fakeGraphQL answers every GraphQL request with code and body.
func fakeGraphQL(t *testing.T, code int, body string) {
	t.Helper()
	fakeAPI(t).GraphQL = func(string) (int, string) { return code, body }
}

// fakeReleases gives owner/repo releases with these tags, newest first, a tag
// with "-alpha" or "-rc" in it flagged a prerelease.
func fakeReleases(t *testing.T, tags ...string) *fakegh.Server {
	t.Helper()
	s := fakeAPI(t)
	for _, tag := range slices.Backward(tags) {
		s.AddRelease("owner/repo", fakegh.Release{Tag: tag, Prerelease: strings.Contains(tag, "-alpha") || strings.Contains(tag, "-rc")})
	}
	return s
}

func TestSplitSource(t *testing.T) {
	cases := []struct {
		source  string
		owner   string
		repo    string
		wantErr bool
	}{
		{"github.com/junegunn/fzf", "junegunn", "fzf", false},
		{"github.com/cli/cli", "cli", "cli", false},
		{"ghe.example.com/myorg/mytool", "myorg", "mytool", false},
		{"github.com/", "", "", true},
		{"notgithub", "", "", true},
		{"github.com/onlyone", "", "", true},
	}
	for _, c := range cases {
		owner, repo, err := SplitSource(c.source)
		if c.wantErr {
			if err == nil {
				t.Errorf("SplitSource(%q) expected error", c.source)
			}
			continue
		}
		if err != nil {
			t.Errorf("SplitSource(%q) unexpected error: %v", c.source, err)
			continue
		}
		if owner != c.owner || repo != c.repo {
			t.Errorf("SplitSource(%q) = (%q, %q), want (%q, %q)", c.source, owner, repo, c.owner, c.repo)
		}
	}
}

// a token GitHub turns away is asked for again at a terminal, once, and the
// request retried with the new one
func TestAuthFailure_AsksOnceForANewTokenWhenInteractive(t *testing.T) {
	s := fakeReleases(t, "v1.0.0")
	t.Setenv("GH_TOKEN", "")
	ui.SetInput(strings.NewReader(fakegh.Token + "\n"))
	t.Cleanup(func() { ui.SetNonInteractive(false) })

	rel, err := GetLatestRelease(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "v1.0.0" {
		t.Errorf("expected the retried call to succeed, got %+v", rel)
	}
	if n := len(s.Calls()); n != 1 {
		t.Errorf("with no token there is nothing to send until one is given: %d calls", n)
	}
}

// with nobody to ask, a refused token surfaces once, with no retry
func TestAuthFailure_NoRetryWhenNotInteractive(t *testing.T) {
	s := fakeReleases(t, "v1.0.0")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	if _, err := ghapi.SaveToken("stale"); err != nil {
		t.Fatal(err)
	}
	ui.SetNonInteractive(true)
	t.Cleanup(func() { ui.SetNonInteractive(false) })

	_, err := GetLatestRelease(context.Background(), "owner", "repo")
	if err == nil || !strings.Contains(err.Error(), "ghpm auth login") {
		t.Fatalf("expected the auth error, saying how to log in: %v", err)
	}
	if n := len(s.Calls()); n != 1 {
		t.Errorf("expected exactly one request (no retry) when not interactive, got %d", n)
	}
}

func TestGetLatestRelease(t *testing.T) {
	s := fakeAPI(t)
	s.AddRelease("owner/repo", fakegh.Release{Tag: "v1.2.3", Assets: []fakegh.Asset{{Name: "tool-linux-amd64.tar.gz", Body: []byte("1234"), Digest: "sha256:ab"}}})

	rel, err := GetLatestRelease(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "v1.2.3" || len(rel.Assets) != 1 {
		t.Fatalf("got %+v", rel)
	}
	a := rel.Assets[0]
	if a.Name != "tool-linux-amd64.tar.gz" || a.Size != 4 || a.Digest != "sha256:ab" || a.ID == 0 {
		t.Errorf("unexpected asset: %+v", a)
	}
}

func TestGetLatestRelease_PrefersNormalVSemverStream(t *testing.T) {
	s := fakeAPI(t)
	for _, r := range []fakegh.Release{
		{Tag: "v2.42.1"},
		{Tag: "v2.44.0-rc.1", Prerelease: true},
		{Tag: "v2.43.0", Assets: []fakegh.Asset{{Name: "dua-v2.43.0-x86_64-pc-windows-msvc.zip", Body: []byte("x")}}},
		{Tag: "dua-core-v3.3.0"},
	} {
		s.AddRelease("any/repo", r)
	}
	s.Latest["any/repo"] = "dua-core-v3.3.0"

	rel, err := GetLatestRelease(context.Background(), "any", "repo")
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "v2.43.0" {
		t.Errorf("expected normal v<semver> tag v2.43.0, got %s", rel.TagName)
	}
	if len(rel.Assets) != 1 {
		t.Errorf("expected selected release assets, got %v", rel.Assets)
	}
}

// a version finds its release under whatever prefix the repo tags by
func TestGetReleaseByTag_AVersionFindsItsTagUnderAnyPrefix(t *testing.T) {
	for _, tc := range []struct{ ver, tag string }{
		{"1.2.3.4", "bun-v1.2.3.4"},
		{"1234", "b1234"},
		{"0.167.0", "rust-v0.167.0"},
		{"0.74.5", "v0.74.5"},
		{"1.8.1", "jq-1.8.1"},
	} {
		fakeReleases(t, "other-9.9.9", tc.tag)
		rel, err := GetReleaseByTag(context.Background(), "owner", "repo", tc.ver)
		if err != nil {
			t.Fatalf("%s: %v", tc.ver, err)
		}
		if rel.TagName != tc.tag {
			t.Errorf("%s: got %s, want %s", tc.ver, rel.TagName, tc.tag)
		}
	}
}

// a real tag is fetched as given, with no list
func TestGetReleaseByTag_ATagIsUsedAsGiven(t *testing.T) {
	s := fakeReleases(t, "rust-v0.167.0")
	rel, err := GetReleaseByTag(context.Background(), "owner", "repo", "rust-v0.167.0")
	if err != nil || rel.TagName != "rust-v0.167.0" {
		t.Fatalf("got %v, %v", rel.TagName, err)
	}
	for _, c := range s.Calls() {
		if strings.HasSuffix(c, "/releases") {
			t.Errorf("listed releases for a real tag: %v", s.Calls())
		}
	}
}

// TestGetReleaseByTag_AllAttemptsFail confirms an error surfaces (not a
// silent empty release) when no candidate tag resolves.
func TestGetReleaseByTag_AllAttemptsFail(t *testing.T) {
	fakeReleases(t, "v1.0.0")
	if _, err := GetReleaseByTag(context.Background(), "owner", "repo", "1.2.3"); err == nil {
		t.Fatal("expected error")
	}
}

// TestFindLatestMatching covers the version-selection logic: given several
// releases, pick the highest one matching the constraint, then fetch that
// release's full details.
func TestFindLatestMatching(t *testing.T) {
	fakeReleases(t, "v2.1.0", "v2.2.0-rc.1", "v1.9.0")
	c, err := config.ParseConstraint("2")
	if err != nil {
		t.Fatal(err)
	}
	rel, err := FindLatestMatching(context.Background(), "owner", "repo", c)
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "v2.1.0" {
		t.Errorf("expected v2.1.0 (highest non-prerelease major-2 release), got %s", rel.TagName)
	}
}

func TestFindLatestMatching_SkipsAPrereleaseNameGitHubDoesNotFlag(t *testing.T) {
	s := fakeAPI(t)
	for _, tag := range []string{"v1.2.0", "v1.3.0", "v1.4.0-rc.1"} {
		s.AddRelease("owner/repo", fakegh.Release{Tag: tag})
	}
	c, _ := config.ParseConstraint("1")
	rel, err := FindLatestMatching(context.Background(), "owner", "repo", c)
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "v1.3.0" {
		t.Errorf("got %s, want v1.3.0: a pin takes no pre-release, flagged or only named", rel.TagName)
	}
}

func TestFindLatestMatching_NoMatch(t *testing.T) {
	fakeReleases(t, "v1.9.0")
	c, err := config.ParseConstraint("2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FindLatestMatching(context.Background(), "owner", "repo", c); err == nil {
		t.Fatal("expected no-match error")
	}
}

// TestGetLatestRelease_RejectsUnsafeAssetName guards the actual boundary:
// every downstream filepath.Join using an asset name (cache path, extract
// path) relies on this rejecting a crafted name before it ever gets that far.
func TestGetLatestRelease_RejectsUnsafeAssetName(t *testing.T) {
	s := fakeAPI(t)
	s.AddRelease("owner/repo", fakegh.Release{Tag: "v1.2.3", Assets: []fakegh.Asset{{Name: "../evil"}}})
	if _, err := GetLatestRelease(context.Background(), "owner", "repo"); err == nil {
		t.Fatal("expected error for unsafe asset name")
	}
}

// releases come back newest first, past one page, and no more than asked for
func TestListReleases_Pages(t *testing.T) {
	var tags []string
	for i := 250; i > 0; i-- {
		tags = append(tags, fmt.Sprintf("v1.0.%d", i))
	}
	fakeReleases(t, tags...)

	releases, err := ListReleases(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 200 || releases[0].TagName != "v1.0.250" || releases[199].TagName != "v1.0.51" {
		t.Errorf("got %d releases, %s first and %s last", len(releases), releases[0].TagName, releases[len(releases)-1].TagName)
	}
}

func TestDownloadAsset(t *testing.T) {
	s := fakeAPI(t)
	s.AddRelease("owner/repo", fakegh.Release{Tag: "v1.0.0", Assets: []fakegh.Asset{
		{Name: "tool-linux-amd64.tar.gz", Body: []byte("linux")},
		{Name: "tool-darwin-arm64.tar.gz", Body: []byte("darwin")},
		{Name: "SHA256SUMS", Body: []byte("sums")},
	}})
	dest := t.TempDir()
	if err := DownloadAsset(context.Background(), "owner", "repo", "v1.0.0", "tool-*-amd64.tar.gz", dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "tool-linux-amd64.tar.gz"))
	if err != nil || string(got) != "linux" {
		t.Errorf("got %q, %v", got, err)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 1 {
		t.Errorf("only the matching asset is written, and nothing beside it: %v", entries)
	}
	if err := DownloadAsset(context.Background(), "owner", "repo", "v1.0.0", "nothing-*", dest); err == nil {
		t.Error("a pattern no asset matches is an error")
	}
}

func TestRateLimitDetection(t *testing.T) {
	s := fakeReleases(t, "v1.0.0")
	s.Fail = func(*http.Request) (int, string) { return 403, `{"message":"API rate limit exceeded for user"}` }
	_, err := GetLatestRelease(context.Background(), "owner", "repo")
	if !IsRateLimited(err) {
		t.Errorf("expected rate limited error, got: %v", err)
	}
}

func TestNormalErrorIsNotARateLimit(t *testing.T) {
	s := fakeReleases(t, "v1.0.0")
	s.Fail = func(*http.Request) (int, string) { return 502, "bad gateway" }
	_, err := GetLatestRelease(context.Background(), "owner", "repo")
	if err == nil || IsRateLimited(err) {
		t.Errorf("expected a plain error, got: %v", err)
	}
}

func TestReleaseStreamRank(t *testing.T) {
	cases := []struct {
		tag        string
		prerelease bool
		wantRank   int
	}{
		{"v2.43.0", false, 0},
		{"dua-core-v3.3.0", false, 1},
		{"bun-v1.3.13", false, 1},
		{"b1234", false, 2},
		{"v2.44.0-rc.1", true, 4},
		{"2.43.0", false, 3},
	}
	for _, c := range cases {
		if got := releaseStreamRank(Release{TagName: c.tag, IsPrerelease: c.prerelease}); got != c.wantRank {
			t.Errorf("releaseStreamRank(%q, prerelease=%v) = %d, want %d", c.tag, c.prerelease, got, c.wantRank)
		}
	}
}

// fakeReleases answers gh release list with tags (stable unless its name says "-alpha"), and gh release view for any
// of them
// two products of one repo tag the same version: the one with the latest stable release's prefix
func TestTagForVersion_PrefersTheLatestStablePrefix(t *testing.T) {
	releases := []Release{
		{TagName: "sdk-v2.0.0-alpha.1", IsPrerelease: true},
		{TagName: "cli-v1.3.0"},
		{TagName: "sdk-v1.2.0"},
		{TagName: "cli-v1.2.0"},
	}
	if got, ok := tagForVersion(releases, "1.2.0"); !ok || got != "cli-v1.2.0" {
		t.Errorf("got %q, %v; want cli-v1.2.0", got, ok)
	}
	if _, ok := tagForVersion(releases, "9.9.9"); ok {
		t.Error("a version with no release was found")
	}
}

// TestGetReleaseByTag_AllAttemptsFail confirms an error surfaces (not a
// silent empty release) when no candidate tag resolves.
func TestValidAssetName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"tool-linux-amd64.tar.gz", true},
		{"", false},
		{".", false},
		{"..", false},
		{"../evil", false},
		{"sub/evil", false},
		{"/etc/passwd", false},
	}
	for _, c := range cases {
		if got := validAssetName(c.name); got != c.want {
			t.Errorf("validAssetName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestGetLatestRelease_RejectsUnsafeAssetName guards the actual boundary:
// every downstream filepath.Join using an asset name (cache path, extract
// path) relies on this rejecting a crafted name before it ever gets that far.
func TestIsRateLimited(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"gh: API rate limit exceeded", true},
		{"gh: HTTP 403: rate limit (remaining 0)", true},
		{"gh: some other error", false},
		{"", false},
		{"gh: Rate Limit Exceeded", true},
	}
	for _, c := range cases {
		err := fmt.Errorf("gh: %s", c.input)
		got := IsRateLimited(err)
		if got != c.want {
			t.Errorf("IsRateLimited(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestIsRateLimited_NilError(t *testing.T) {
	if IsRateLimited(nil) {
		t.Error("IsRateLimited(nil) should be false")
	}
}
