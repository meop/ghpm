package gh

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/ghbin"
	"github.com/meop/ghpm/internal/ui"
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

// fakeGH stamps a fake `gh` script directly at ghpm's vendored gh path — the
// only gh ghpm ever runs internally now, so tests plant it there rather than
// on PATH. Isolates HOME itself so every caller gets it for free.
func fakeGH(t *testing.T, script string) string {
	t.Helper()
	withTestHome(t)
	vendored, err := ghbin.VendorPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(vendored), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vendored, []byte("#!/bin/sh\n"+script+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(vendored)
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

// TestRunCmd_RetriesOnceAfterAuthFailureWhenInteractive covers the reactive
// half of auth handling: a call that fails looking like a bad/expired
// credential gets one retry, after a (here, faked) re-auth — rather than
// leaving a dead token to fail every command after it too.
func TestRunCmd_RetriesOnceAfterAuthFailureWhenInteractive(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "retried")
	t.Setenv("RETRY_MARKER", marker)

	fakeGH(t, `
		case "$*" in
			*"auth login"*)
				exit 0
				;;
			*"release view"*)
				if [ -f "$RETRY_MARKER" ]; then
					echo '{"tagName":"v1.0.0","assets":[]}'
				else
					touch "$RETRY_MARKER"
					echo "gh: Bad credentials (HTTP 401)" >&2
					exit 1
				fi
				;;
		esac
	`)
	ui.SetInput(strings.NewReader(""))
	t.Cleanup(func() { ui.SetNonInteractive(false) })

	rel, err := GetLatestRelease(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "v1.0.0" {
		t.Errorf("expected the retried call to succeed, got %+v", rel)
	}
}

// TestRunCmd_NoRetryWhenNotInteractive covers the other half: with nobody to
// answer a re-auth prompt, the original auth error surfaces once rather than
// hanging or looping.
func TestRunCmd_NoRetryWhenNotInteractive(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls")
	t.Setenv("CALL_LOG", calls)

	fakeGH(t, `
		echo x >> "$CALL_LOG"
		echo "gh: Bad credentials (HTTP 401)" >&2
		exit 1
	`)
	// force non-interactive regardless of test order/leakage from
	// ui.SetInput's interactiveOverride, which has no reset of its own
	ui.SetNonInteractive(true)
	t.Cleanup(func() { ui.SetNonInteractive(false) })

	_, err := GetLatestRelease(context.Background(), "owner", "repo")
	if err == nil {
		t.Fatal("expected the auth error to surface")
	}
	body, _ := os.ReadFile(calls)
	if got := len(strings.TrimSpace(string(body))); got != 1 {
		t.Errorf("expected exactly one call attempt (no retry) when not interactive, got %d", got)
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

func TestGetLatestRelease_PrefersNormalVSemverStream(t *testing.T) {
	fakeGH(t, `
		case "$1 $2 $3" in
			"release view -R") echo '{"tagName":"dua-core-v3.3.0","assets":[]}' ;;
			"release list -R") echo '[{"tagName":"dua-core-v3.3.0","isPrerelease":false},{"tagName":"v2.43.0","isPrerelease":false},{"tagName":"v2.44.0-rc.1","isPrerelease":true},{"tagName":"v2.42.1","isPrerelease":false}]' ;;
			"release view v2.43.0") echo '{"tagName":"v2.43.0","assets":[{"name":"dua-v2.43.0-x86_64-pc-windows-msvc.zip","size":123,"url":"https://x.com/a"}]}' ;;
			*) echo "unexpected gh invocation: $*" >&2; exit 1 ;;
		esac
	`)

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

func TestCandidateTags(t *testing.T) {
	got := candidateTags("bun", "v1.2.3.4")
	want := []string{"1.2.3.4", "v1.2.3.4", "bun-v1.2.3.4", "b1.2.3.4", "bun-b1.2.3.4"}
	if !slices.Equal(got, want) {
		t.Errorf("candidateTags(%q, %q) = %v, want %v", "bun", "v1.2.3.4", got, want)
	}
}

// TestGetReleaseByTag_FallsBackThroughCandidates covers pinning by a bare
// version against a repo that tags releases with a package-name prefix
// (bun's "bun-v1.2.3.4"): the exact tag and the plain "v"-prefixed guess both
// miss, so it must keep trying rather than give up after one retry.
func TestGetReleaseByTag_FallsBackThroughCandidates(t *testing.T) {
	fakeGH(t, `
		for a in "$@"; do
			if [ "$a" = "bun-v1.2.3.4" ]; then
				echo '{"tagName":"bun-v1.2.3.4","assets":[]}'
				exit 0
			fi
		done
		exit 1
	`)

	rel, err := GetReleaseByTag(context.Background(), "owner", "bun", "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "bun-v1.2.3.4" {
		t.Errorf("expected tag bun-v1.2.3.4, got %s", rel.TagName)
	}
}

// TestGetReleaseByTag_BareBuildNumber covers llama.cpp's tagging: a bare
// "b<n>" build number with no "v" and no package-name prefix at all.
func TestGetReleaseByTag_BareBuildNumber(t *testing.T) {
	fakeGH(t, `
		for a in "$@"; do
			if [ "$a" = "b1234" ]; then
				echo '{"tagName":"b1234","assets":[]}'
				exit 0
			fi
		done
		exit 1
	`)

	rel, err := GetReleaseByTag(context.Background(), "owner", "llama.cpp", "1234")
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "b1234" {
		t.Errorf("expected tag b1234, got %s", rel.TagName)
	}
}

// TestGetReleaseByTag_AllAttemptsFail confirms an error surfaces (not a
// silent empty release) when no candidate tag resolves.
func TestGetReleaseByTag_AllAttemptsFail(t *testing.T) {
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
	withTestHome(t)

	// an already-expired context fails the vendor bootstrap's fetch without
	// touching the network, deterministically simulating "offline"
	ctx, cancel := context.WithDeadline(context.Background(), time.Now())
	defer cancel()

	if err := CheckInstalled(ctx); err == nil {
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
