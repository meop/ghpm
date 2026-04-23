package gh

import (
	"os"
	"os/exec"
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
	fakeGH(t, `echo '[{"tagName":"v1.2.3","assets":[{"name":"tool-linux-amd64.tar.gz","size":1234,"url":"https://example.com/tool.tar.gz"}],"isLatest":true}]'
# gh release view outputs a single object, not array
echo '{"tagName":"v1.2.3","assets":[{"name":"tool-linux-amd64.tar.gz","size":1234,"url":"https://example.com/tool.tar.gz"}],"isLatest":true}'`)

	// The fake gh ignores args and always prints the second echo (release view)
	// We verify our JSON parsing works with known output
	fakeGH(t, `echo '{"tagName":"v1.2.3","assets":[{"name":"tool-linux-amd64.tar.gz","size":1234,"url":"https://x.com/a"}],"isLatest":true}'`)

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
	fakeGH(t, `echo '[{"tagName":"v2.0.0","isLatest":true},{"tagName":"v1.0.0","isLatest":false}]'`)

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
	if !releases[0].IsLatest {
		t.Error("expected first release to be latest")
	}
}

func TestCheckInstalled_NotFound(t *testing.T) {
	// Override PATH to a dir with no gh binary
	empty := t.TempDir()
	t.Setenv("PATH", empty)

	// exec.LookPath uses the current PATH — reset exec cache by checking directly
	_, err := exec.LookPath("gh")
	if err == nil {
		t.Skip("gh found in restricted PATH (unexpected)")
	}

	if err := CheckInstalled(); err == nil {
		t.Error("expected error when gh not found")
	}
}
