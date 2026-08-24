package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/meop/ghpm/internal/ghbin"
	"github.com/meop/ghpm/internal/store"
	"github.com/meop/ghpm/internal/ui"
)

func withHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GHPM_TEST_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("LOCALAPPDATA", filepath.Join(dir, "AppData", "Local"))
	return dir
}

func writeRepoTOML(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "repo.toml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// fakeGHBin stamps a fake `gh` script directly at ghpm's vendored gh path —
// the only gh ghpm ever runs internally now. Callers must have already
// isolated HOME (withHome) so this resolves to a test-owned path.
func fakeGHBin(t *testing.T, script string) {
	t.Helper()
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
}

func writeConfigTOML(t *testing.T, s *Settings) {
	t.Helper()
	dir, err := store.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := toml.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRepos_Empty(t *testing.T) {
	withHome(t)
	repos, err := LoadRepos()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("expected empty map, got %v", repos)
	}
}

func TestLoadRepos_Single(t *testing.T) {
	withHome(t)
	base, err := store.ReposBaseDir()
	if err != nil {
		t.Fatal(err)
	}
	writeRepoTOML(t, filepath.Join(base, "a"), "fzf = \"github.com/junegunn/fzf\"\n")
	repos, err := LoadRepos()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repos["fzf"] != "github.com/junegunn/fzf" {
		t.Errorf("got %v", repos)
	}
}

func TestLoadRepos_AlphabeticalOrder_LaterWins(t *testing.T) {
	withHome(t)
	base, err := store.ReposBaseDir()
	if err != nil {
		t.Fatal(err)
	}
	writeRepoTOML(t, filepath.Join(base, "a"), "tool = \"github.com/owner/a\"\n")
	writeRepoTOML(t, filepath.Join(base, "b"), "tool = \"github.com/owner/b\"\n")
	repos, err := LoadRepos()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repos["tool"] != "github.com/owner/b" {
		t.Errorf("expected later file to win, got %q", repos["tool"])
	}
}

func TestRefreshRepos_FetchesAllSourcesAndPreservesOrder(t *testing.T) {
	withHome(t)
	writeConfigTOML(t, &Settings{RepoSources: []string{"github.com/o/a", "github.com/o/b"}})

	fakeGHBin(t, `case "$*" in
  *"repos/o/a/contents/repo.toml"*)
    echo 'pkg1 = "github.com/o/pkg1"'
    ;;
  *"repos/o/b/contents/repo.toml"*)
    printf 'pkg1 = "github.com/o/pkg1"\npkg2 = "github.com/o/pkg2"\n'
    ;;
  *) exit 1 ;;
esac`)

	results, err := RefreshRepos()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// RefreshRepos fetches sources concurrently but must still report results in
	// the original configured order, not completion order.
	if results[0].Source != "github.com/o/a" || results[1].Source != "github.com/o/b" {
		t.Fatalf("expected results in source order, got %+v", results)
	}
	if results[0].Count != 1 {
		t.Errorf("expected 1 entry for source a, got %d", results[0].Count)
	}
	if results[1].Count != 2 {
		t.Errorf("expected 2 entries for source b, got %d", results[1].Count)
	}
}

func TestRefreshRepos_PartialFailurePreservesOtherResults(t *testing.T) {
	withHome(t)
	writeConfigTOML(t, &Settings{RepoSources: []string{"github.com/o/good", "github.com/o/bad"}})

	fakeGHBin(t, `case "$*" in
  *"repos/o/good/contents/repo.toml"*)
    echo 'pkg1 = "github.com/o/pkg1"'
    ;;
  *"repos/o/bad/contents/repo.toml"*)
    echo "not found" >&2
    exit 1
    ;;
  *) exit 1 ;;
esac`)

	results, err := RefreshRepos()
	if err == nil {
		t.Fatal("expected an error since one source failed")
	}
	if len(results) != 2 {
		t.Fatalf("expected both results even with a partial failure, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Errorf("expected source 'good' to succeed, got %v", results[0].Err)
	}
	if results[1].Err == nil {
		t.Error("expected source 'bad' to have an error")
	}
}

func TestLoadRepos_InvalidTOML_Fatal(t *testing.T) {
	withHome(t)
	base, err := store.ReposBaseDir()
	if err != nil {
		t.Fatal(err)
	}
	writeRepoTOML(t, filepath.Join(base, "bad"), "fzf = \"unterminated\ninvalid toml {{{\n")
	_, err = LoadRepos()
	if err == nil {
		t.Error("expected error for invalid TOML, got nil")
	}
}

func setStdin(t *testing.T, input string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	ui.SetInput(r)
	t.Cleanup(func() { ui.SetInput(os.Stdin) })
	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
}

func TestParseVersionSuffix(t *testing.T) {
	cases := []struct {
		input   string
		name    string
		version string
		pinned  bool
	}{
		{"fzf", "fzf", "", false},
		{"fzf@0.70", "fzf", "0.70", true},
		{"fzf@v0.70.0", "fzf", "v0.70.0", true},
	}
	for _, c := range cases {
		name, ver, pinned := ParseVersionSuffix(c.input)
		if name != c.name || ver != c.version || pinned != c.pinned {
			t.Errorf("ParseVersionSuffix(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.input, name, ver, pinned, c.name, c.version, c.pinned)
		}
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{"fzf", "gh", "ripgrep", "my-tool", "tool123"}
	invalid := []string{"cli/cli", "github.com/cli/cli", "a b", "", "owner/repo"}

	for _, n := range valid {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q) unexpected error: %v", n, err)
		}
	}
	for _, n := range invalid {
		if err := ValidateName(n); err == nil {
			t.Errorf("ValidateName(%q) expected error, got nil", n)
		}
	}
}

func TestResolveSource_ManifestAndRepos(t *testing.T) {
	m := &Manifest{
		Repos: map[string]string{
			"bat": "github.com/sharkdp/bat",
		},
		Extracts: map[string]PackageEntry{},
	}
	repos := map[string]string{
		"fzf": "github.com/junegunn/fzf",
		"rg":  "github.com/BurntSushi/ripgrep",
	}

	cases := []struct {
		name    string
		version string
		want    string
		wantErr bool
	}{
		// From manifest repos
		{"bat", "", "github.com/sharkdp/bat", false},
		// From repos
		{"fzf", "", "github.com/junegunn/fzf", false},
		{"rg", "", "github.com/BurntSushi/ripgrep", false},
		// Unknown — falls through to gh search which fails (no gh in test env)
		{"unknowntool", "", "", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.wantErr {
				setStdin(t, "0\n")
			}
			got, err := ResolveSource(c.name, c.version, m, repos)
			if c.wantErr {
				if err == nil {
					t.Errorf("ResolveSource(%q) expected error, got %q", c.name, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ResolveSource(%q) unexpected error: %v", c.name, err)
				return
			}
			if got != c.want {
				t.Errorf("ResolveSource(%q) = %q, want %q", c.name, got, c.want)
			}
		})
	}
}

func TestFindBySource(t *testing.T) {
	m := &Manifest{
		Repos: map[string]string{
			"gh":  "github.com/cli/cli",
			"fzf": "github.com/junegunn/fzf",
		},
		Extracts: map[string]PackageEntry{},
	}

	key, found := m.FindBySource("github.com/cli/cli")
	if !found || key != "gh" {
		t.Errorf("FindBySource(cli/cli) = (%q, %v), want (\"gh\", true)", key, found)
	}

	_, found = m.FindBySource("github.com/nobody/nothere")
	if found {
		t.Error("FindBySource(nothere) should not be found")
	}
}

// TestSearchGitHub_NonInteractiveFailsWithoutPrompting exercises the default
// test environment (stdin isn't a terminal), the common case: no result
// picker fires, and no gh call is needed to know that.
func TestSearchGitHub_NonInteractiveFailsWithoutPrompting(t *testing.T) {
	_, err := SearchGitHub("some-nonexistent-name")
	if err == nil {
		t.Fatal("expected SearchGitHub to fail rather than prompt when non-interactive")
	}
}

// TestSearchGitHub_ForcedNonInteractiveOverridesInteractiveInput covers the
// actual bug this exists for: a caller (like ghpm's own --non-interactive
// flag) that knows nobody can answer, even though something upstream —
// SetInput here, an inherited terminal in production — makes stdin look
// answerable.
func TestSearchGitHub_ForcedNonInteractiveOverridesInteractiveInput(t *testing.T) {
	ui.SetInput(strings.NewReader("1\n"))
	ui.SetNonInteractive(true)
	t.Cleanup(func() { ui.SetNonInteractive(false) })

	_, err := SearchGitHub("some-nonexistent-name")
	if err == nil {
		t.Fatal("expected forced non-interactive to override an interactive-looking input reader")
	}
}
