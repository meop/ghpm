package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/meop/ghpm/internal/fakegh"
	"github.com/meop/ghpm/internal/store"
	"github.com/meop/golie/ui"
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
	writeRepoTOML(t, filepath.Join(base, "a"), "[fzf]\nuri = \"github.com/junegunn/fzf\"\n")
	repos, err := LoadRepos()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repos["fzf"].URI != "github.com/junegunn/fzf" {
		t.Errorf("got %v", repos)
	}
}

func TestLoadRepos_AlphabeticalOrder_LaterWins(t *testing.T) {
	withHome(t)
	base, err := store.ReposBaseDir()
	if err != nil {
		t.Fatal(err)
	}
	writeRepoTOML(t, filepath.Join(base, "a"), "[tool]\nuri = \"github.com/owner/a\"\n")
	writeRepoTOML(t, filepath.Join(base, "b"), "[tool]\nuri = \"github.com/owner/b\"\n")
	repos, err := LoadRepos()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repos["tool"].URI != "github.com/owner/b" {
		t.Errorf("expected later file to win, got %q", repos["tool"].URI)
	}
}

func TestRefreshRepos_FetchesAllSourcesAndPreservesOrder(t *testing.T) {
	withHome(t)
	writeConfigTOML(t, &Settings{RepoSources: []string{"github.com/o/a", "github.com/o/b"}})
	a := "[pkg1]\nuri = \"github.com/o/pkg1\"\n"
	b := "[pkg1]\nuri = \"github.com/o/pkg1\"\n[pkg2]\nuri = \"github.com/o/pkg2\"\n"

	gh := fakegh.New(t)
	gh.Files["o/a"] = map[string][]byte{"repo.toml": []byte(a)}
	gh.Files["o/b"] = map[string][]byte{"repo.toml": []byte(b)}

	var fetched []string
	var mu sync.Mutex
	results, err := RefreshRepos(func(source, path string) {
		mu.Lock()
		defer mu.Unlock()
		fetched = append(fetched, source+" "+path)
	})
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
	for i, body := range []string{a, b} {
		if r := results[i]; r.From != "" || r.To != short(gitBlobHash([]byte(body))) {
			t.Errorf("%s went %q → %q, want \"\" → its file's short hash", r.Source, r.From, r.To)
		}
	}
	slices.Sort(fetched)
	if !slices.Equal(fetched, []string{"github.com/o/a repo.toml", "github.com/o/b repo.toml"}) {
		t.Errorf("fetching was told %v", fetched)
	}
}

// TestRefreshRepos_ACurrentFileIsNotDownloaded: GitHub's hash for the file is
// asked first, and where it is the copy here's, nothing is fetched.
func TestRefreshRepos_ACurrentFileIsNotDownloaded(t *testing.T) {
	withHome(t)
	writeConfigTOML(t, &Settings{RepoSources: []string{"github.com/o/a"}})
	body := "[pkg1]\nuri = \"github.com/o/pkg1\"\n"
	dir, err := store.RepoDir("github.com/o/a")
	if err != nil {
		t.Fatal(err)
	}
	writeRepoTOML(t, dir, body)

	gh := fakegh.New(t)
	gh.Files["o/a"] = map[string][]byte{"repo.toml": []byte(body)}

	results, err := RefreshRepos(func(source, path string) { t.Errorf("fetched %s %s", source, path) })
	if err != nil {
		t.Fatal(err)
	}
	if r := results[0]; r.From != short(gitBlobHash([]byte(body))) || r.To != r.From {
		t.Errorf("went %q → %q, want its short hash twice", r.From, r.To)
	}
}

func TestRefreshRepos_PartialFailurePreservesOtherResults(t *testing.T) {
	withHome(t)
	writeConfigTOML(t, &Settings{RepoSources: []string{"github.com/o/good", "github.com/o/bad"}})

	gh := fakegh.New(t)
	gh.Files["o/good"] = map[string][]byte{"repo.toml": []byte("[pkg1]\nuri = \"github.com/o/pkg1\"\n")}

	results, err := RefreshRepos(nil)
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
	repos := map[string]RepoEntry{
		"fzf": {URI: "github.com/junegunn/fzf"},
		"rg":  {URI: "github.com/BurntSushi/ripgrep"},
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

// GitHub's search is only candidates: add and info keep the repos named exactly
// what was typed, whoever owns them, and find keeps the ones the term starts a
// word of. Neither keeps a repo for holding the term somewhere inside a word.
func TestHits_HeldToTheTwoRules(t *testing.T) {
	hits := []RepoHit{
		{FullName: "junegunn/fzf", Descr: "A command-line fuzzy finder"},
		{FullName: "junegunn/fzf.vim", Descr: "fzf and vim"},
		{FullName: "someone/FZF", Descr: ""},
		{FullName: "fzf/dotfiles", Descr: "my config"},
		{FullName: "other/skimfzf", Descr: "nothing to do with it"},
	}
	full := func(hs []RepoHit) []string {
		names := make([]string, len(hs))
		for i, h := range hs {
			names[i] = h.FullName
		}
		return names
	}

	// an owner called fzf is not a repo called fzf, and neither is fzf.vim
	if got, want := full(HitsNamed(hits, "fzf")), []string{"junegunn/fzf", "someone/FZF"}; !slices.Equal(got, want) {
		t.Errorf("HitsNamed(fzf) = %v, want %v", got, want)
	}
	if got := HitsNamed(hits, "fz"); len(got) != 0 {
		t.Errorf("HitsNamed(fz) = %v, want none: part of a name is not the name", full(got))
	}

	// the start of a word of owner/repo or of the description, in GitHub's order
	want := []string{"junegunn/fzf", "junegunn/fzf.vim", "someone/FZF", "fzf/dotfiles"}
	if got := full(HitsFound(hits, "fzf")); !slices.Equal(got, want) {
		t.Errorf("HitsFound(fzf) = %v, want %v", got, want)
	}
	if got, want := full(HitsFound(hits, "fuzzy")), []string{"junegunn/fzf"}; !slices.Equal(got, want) {
		t.Errorf("HitsFound(fuzzy) = %v, want %v", got, want)
	}
	if got := HitsFound(hits, "inder"); len(got) != 0 {
		t.Errorf("HitsFound(inder) = %v, want none: the end of a word is not its start", full(got))
	}

	// what is shown is capped, since a name like dotfiles is thousands of repos
	var many []RepoHit
	for i := range searchShow + 5 {
		many = append(many, RepoHit{FullName: fmt.Sprintf("owner%d/dotfiles", i)})
	}
	if got := HitsNamed(many, "dotfiles"); len(got) != searchShow {
		t.Errorf("HitsNamed kept %d, want the first %d", len(got), searchShow)
	}
}

// The pick add and info offer is over repos named exactly what was typed: a
// search that ranks fzf.vim above fzf does not get to offer it, and a term
// GitHub has plenty to say about but no repo is named is not found.
func TestSearchGitHub_OffersOnlyExactNames(t *testing.T) {
	withHome(t)
	fzf := fakegh.Hit{FullName: "junegunn/fzf", Description: "A command-line fuzzy finder"}
	fakegh.New(t).Search = func(q string) []fakegh.Hit {
		switch q {
		case "fzf in:name":
			return []fakegh.Hit{{FullName: "junegunn/fzf.vim", Description: "fzf and vim"}, fzf}
		case "fuzzy in:name":
			return []fakegh.Hit{fzf}
		case "fuzzy in:name,description":
			return []fakegh.Hit{fzf, {FullName: "other/unfuzzy"}}
		}
		return nil
	}
	var out bytes.Buffer
	ui.SetOutput(&out)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	ui.SetInput(strings.NewReader("1\n"))
	got, err := SearchGitHub("fzf")
	if err != nil || got != "github.com/junegunn/fzf" {
		t.Errorf("SearchGitHub(fzf) = %q, %v; want the one repo named fzf", got, err)
	}
	if strings.Contains(out.String(), "fzf.vim") {
		t.Errorf("fzf.vim is not named fzf and should not be offered:\n%s", out.String())
	}

	ui.SetInput(strings.NewReader("1\n"))
	if got, err := SearchGitHub("fuzzy"); err == nil || !strings.Contains(err.Error(), "ghpm find fuzzy") {
		t.Errorf("SearchGitHub(fuzzy) = %q, %v; want not found, pointing at find", got, err)
	}

	// find's search reads the description too, and still drops the middle of a word
	hits, err := SearchReposFound("fuzzy")
	if err != nil || len(hits) != 1 || hits[0].FullName != "junegunn/fzf" {
		t.Errorf("SearchReposFound(fuzzy) = %v, %v; want junegunn/fzf alone", hits, err)
	}
}

// TestLoadRepos_TableForm covers the registry's entry shape, where a package is
// a table carrying uri and descr.
func TestLoadRepos_TableForm(t *testing.T) {
	withHome(t)
	base, err := store.ReposBaseDir()
	if err != nil {
		t.Fatal(err)
	}
	writeRepoTOML(t, filepath.Join(base, "a"), `
[fzf]
uri = "github.com/junegunn/fzf"
descr = "Command-line fuzzy finder."

[bat]
uri = "github.com/sharkdp/bat"
`)
	repos, err := LoadRepos()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repos["fzf"].URI != "github.com/junegunn/fzf" || repos["fzf"].Descr != "Command-line fuzzy finder." {
		t.Errorf("table entry: got %+v", repos["fzf"])
	}
	if repos["bat"].URI != "github.com/sharkdp/bat" || repos["bat"].Descr != "" {
		t.Errorf("descr-less table entry: got %+v", repos["bat"])
	}
}

func TestLoadRepos_MalformedEntry(t *testing.T) {
	withHome(t)
	base, err := store.ReposBaseDir()
	if err != nil {
		t.Fatal(err)
	}
	writeRepoTOML(t, filepath.Join(base, "a"), "fzf = 42\n")
	if _, err := LoadRepos(); err == nil {
		t.Error("expected an error for an entry that is not a table")
	}
}

func TestParseRepoTable_Aliases(t *testing.T) {
	entries, err := parseRepoTable([]byte(`
[rg]
uri = "github.com/BurntSushi/ripgrep"
descr = "Line-oriented regex search tool."
aliases = ["ripgrep", ""]

[fzf]
uri = "github.com/junegunn/fzf"
`), "repo.toml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// a blank is not a name, and is dropped
	if got := entries["rg"].Aliases; len(got) != 1 || got[0] != "ripgrep" {
		t.Errorf("aliases: got %v", got)
	}
	if got := entries["fzf"].Aliases; len(got) != 0 {
		t.Errorf("an entry without aliases has none: got %v", got)
	}
}

func TestParseRepoTable_RefusesWhatItDoesNotKnow(t *testing.T) {
	for _, body := range []string{
		"fzf = \"github.com/junegunn/fzf\"\n",
		"[fzf]\nuri = \"github.com/junegunn/fzf\"\nhomepage = \"x\"\n",
		"[fzf]\nuri = \"github.com/junegunn/fzf\"\naliases = [7]\n",
	} {
		if _, err := parseRepoTable([]byte(body), "repo.toml"); err == nil {
			t.Errorf("accepted %q", body)
		}
	}
}

func TestCanonicalName(t *testing.T) {
	m := &Manifest{Repos: map[string]string{"bat": "github.com/sharkdp/bat"}}
	repos := map[string]RepoEntry{
		"rg":    {URI: "github.com/BurntSushi/ripgrep", Aliases: []string{"ripgrep"}},
		"btm":   {URI: "github.com/ClementTsang/bottom", Aliases: []string{"bottom"}},
		"other": {URI: "github.com/x/other", Aliases: []string{"bottom", "rg"}},
	}
	cases := []struct{ typed, want string }{
		// a key is itself, whether the manifest, the builtins or the registry holds it
		{"bat", "bat"},
		{"gh", "gh"},
		{"rg", "rg"},
		// an alias is another way to say a key, case aside
		{"ripgrep", "rg"},
		{"RipGrep", "rg"},
		{"RG", "rg"},
		// a key wins over an alias of the same spelling, and of two entries claiming an alias the first sorted does
		{"bottom", "btm"},
		// part of a name is not a name, and neither is a second spelling of one
		{"rip", "rip"},
		{"rip-grep", "rip-grep"},
		{"nosuch", "nosuch"},
	}
	for _, c := range cases {
		if got := CanonicalName(c.typed, m, repos); got != c.want {
			t.Errorf("CanonicalName(%q) = %q, want %q", c.typed, got, c.want)
		}
	}
	if got := CanonicalName("ripgrep", nil, nil); got != "ripgrep" {
		t.Errorf("with no registry a name is itself: got %q", got)
	}
}
