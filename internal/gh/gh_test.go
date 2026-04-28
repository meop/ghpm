package gh

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// fakeGH writes a fake `gh` script that prints JSON to stdout.
func fakeGH(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "gh")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return dir
}

func TestSplitSource(t *testing.T) {
	cases := []struct {
		source    string
		owner     string
		repo      string
		wantErr   bool
	}{
		{"github.com/junegunn/fzf", "junegunn", "fzf", false},
		{"github.com/cli/cli", "cli", "cli", false},
		{"github.com/", "", "", true},
		{"notgithub", "", "", true},
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

	rel, err := GetLatestRelease("owner", "repo")
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

func TestListReleases_MockGH(t *testing.T) {
	fakeGH(t, `echo '[{"tagName":"v2.0.0"},{"tagName":"v1.0.0"}]'`)

	releases, err := ListReleases("owner", "repo")
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
	_, err := GetLatestRelease("owner", "repo")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsRateLimited(err) {
		t.Errorf("expected rate limited error, got: %v", err)
	}
}

func TestRun_NormalError(t *testing.T) {
	fakeGH(t, `echo "some network error" >&2 && exit 1`)
	_, err := GetLatestRelease("owner", "repo")
	if err == nil {
		t.Fatal("expected error")
	}
	if IsRateLimited(err) {
		t.Errorf("should not be rate limited, got: %v", err)
	}
}
