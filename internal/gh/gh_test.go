package gh

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/meop/ghpm/internal/config"
)

// fakeGH writes a fake `gh` script that prints JSON to stdout.
func fakeGH(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "gh")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	path := dir
	if filepath.Separator != '\\' {
		path += string(os.PathListSeparator) + os.Getenv("PATH")
	}
	t.Setenv("PATH", path)
	return dir
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

func TestGetLatestRelease_MockGH(t *testing.T) {
	fakeGH(t, `echo '{"tagName":"v1.2.3","assets":[{"name":"tool-linux-amd64.tar.gz","size":1234,"url":"https://x.com/a"}]}'`)

	rel, err := GetLatestRelease(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "v1.2.3" {
		t.Errorf("expected tag v1.2.3, got %s", rel.TagName)
	}
	if len(rel.Assets) != 1 {
		t.Errorf("expected 1 asset, got %d", len(rel.Assets))
	}
	if rel.Assets[0].Name != "tool-linux-amd64.tar.gz" {
		t.Errorf("unexpected asset name: %s", rel.Assets[0].Name)
	}
}

func TestAlternateVTag(t *testing.T) {
	cases := []struct{ in, want string }{
		{"v1.2.3", "1.2.3"},
		{"1.2.3", "v1.2.3"},
		{"v", ""},
		{"", "v"},
	}
	for _, c := range cases {
		if got := alternateVTag(c.in); got != c.want {
			t.Errorf("alternateVTag(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestGetReleaseByTag_FallsBackToAlternateVTag covers the retry: a tag
// lookup that fails is retried once with the v-prefix toggled (bun tags
// releases "v1.3.13"; some repos tag "1.3.13" instead, or vice versa).
func TestGetReleaseByTag_FallsBackToAlternateVTag(t *testing.T) {
	fakeGH(t, `
		for a in "$@"; do
			if [ "$a" = "v1.2.3" ]; then
				echo '{"tagName":"v1.2.3","assets":[]}'
				exit 0
			fi
		done
		exit 1
	`)

	rel, err := GetReleaseByTag(context.Background(), "owner", "repo", "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "v1.2.3" {
		t.Errorf("expected tag v1.2.3, got %s", rel.TagName)
	}
}

// TestGetReleaseByTag_BothAttemptsFail confirms both errors are surfaced
// (not just the second attempt's) when neither tag spelling resolves.
func TestGetReleaseByTag_BothAttemptsFail(t *testing.T) {
	fakeGH(t, `echo "release not found" >&2 && exit 1`)

	_, err := GetReleaseByTag(context.Background(), "owner", "repo", "1.2.3")
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestFindLatestMatching covers the version-selection logic: given several
// releases, pick the highest one matching the constraint, then fetch that
// release's full details.
func TestFindLatestMatching(t *testing.T) {
	fakeGH(t, `
		case "$1 $2" in
			"release list") echo '[{"tagName":"v2.1.0","isPrerelease":false},{"tagName":"v2.2.0","isPrerelease":true},{"tagName":"v1.9.0","isPrerelease":false}]' ;;
			"release view") echo '{"tagName":"v2.1.0","assets":[]}' ;;
		esac
	`)

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

func TestFindLatestMatching_NoMatch(t *testing.T) {
	fakeGH(t, `echo '[{"tagName":"v1.9.0","isPrerelease":false}]'`)

	c, err := config.ParseConstraint("2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FindLatestMatching(context.Background(), "owner", "repo", c); err == nil {
		t.Fatal("expected no-match error")
	}
}

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
func TestGetLatestRelease_RejectsUnsafeAssetName(t *testing.T) {
	fakeGH(t, `echo '{"tagName":"v1.2.3","assets":[{"name":"../evil","size":1,"url":"https://x.com/a"}]}'`)

	_, err := GetLatestRelease(context.Background(), "owner", "repo")
	if err == nil {
		t.Fatal("expected error for unsafe asset name")
	}
}

func TestListReleases_MockGH(t *testing.T) {
	fakeGH(t, `echo '[{"tagName":"v2.0.0"},{"tagName":"v1.0.0"}]'`)

	releases, err := ListReleases(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 2 {
		t.Fatalf("expected 2 releases, got %d", len(releases))
	}
	if releases[0].TagName != "v2.0.0" {
		t.Errorf("expected v2.0.0, got %s", releases[0].TagName)
	}
}

func TestCheckInstalled_NotFound(t *testing.T) {
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	t.Setenv("HOME", empty)
	t.Setenv("USERPROFILE", empty)

	if err := CheckInstalled(); err == nil {
		t.Error("expected error when gh not found")
	}
}

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

func TestRun_RateLimitDetection(t *testing.T) {
	fakeGH(t, `echo "API rate limit exceeded" >&2 && exit 1`)
	_, err := GetLatestRelease(context.Background(), "owner", "repo")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsRateLimited(err) {
		t.Errorf("expected rate limited error, got: %v", err)
	}
}

func TestRun_NormalError(t *testing.T) {
	fakeGH(t, `echo "some network error" >&2 && exit 1`)
	_, err := GetLatestRelease(context.Background(), "owner", "repo")
	if err == nil {
		t.Fatal("expected error")
	}
	if IsRateLimited(err) {
		t.Errorf("should not be rate limited, got: %v", err)
	}
}
