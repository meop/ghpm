package cli

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/golie/ui"
)

func TestFilterExtracts(t *testing.T) {
	extracts := map[string]config.PackageEntry{
		"fzf":    {Version: "1"},
		"fzf@14": {Version: "2"},
		"rg":     {Version: "3"},
	}
	if got := filterExtracts(extracts, nil); len(got) != 3 {
		t.Errorf("nil names should return all: want 3, got %d", len(got))
	}
	if got := filterExtracts(extracts, []string{"rg"}); len(got) != 1 || got["rg"].Version != "3" {
		t.Errorf("exact key match: got %v", got)
	}
	// A base name matches both the plain and the versioned install.
	if got := filterExtracts(extracts, []string{"fzf"}); len(got) != 2 {
		t.Errorf("base-name match: want fzf and fzf@14, got %v", got)
	}
	if got := filterExtracts(extracts, []string{"fzf@14"}); len(got) != 1 {
		t.Errorf("versioned key match: want 1, got %v", got)
	}
	if got := filterExtracts(extracts, []string{"nope"}); len(got) != 0 {
		t.Errorf("no match: want 0, got %d", len(got))
	}
}

func TestCanonicalKey(t *testing.T) {
	m := &config.Manifest{Repos: map[string]string{"rg": "github.com/BurntSushi/ripgrep"}}
	repos := map[string]config.RepoEntry{"rg": {URI: "github.com/BurntSushi/ripgrep", Aliases: []string{"ripgrep"}}}
	cases := []struct{ typed, want string }{
		{"rg", "rg"},
		{"ripgrep", "rg"},
		// the pin is carried as typed, behind whatever the name resolves to
		{"ripgrep@14", "rg@14"},
		{"rg@14", "rg@14"},
		{"rip", "rip"},
	}
	for _, c := range cases {
		if got := canonicalKey(c.typed, m, repos); got != c.want {
			t.Errorf("canonicalKey(%q) = %q, want %q", c.typed, got, c.want)
		}
	}
}

func TestFindExtracts(t *testing.T) {
	extracts := map[string]config.PackageEntry{
		"fzf":      {Version: "1"},
		"fzf@14":   {Version: "2"},
		"rg":       {Version: "3"},
		"ast-grep": {Version: "4"},
	}
	repos := map[string]config.RepoEntry{"rg": {Aliases: []string{"ripgrep"}}}
	keys := func(terms ...string) string {
		got := findExtracts(extracts, terms, repos)
		names := make([]string, 0, len(got))
		for k := range got {
			names = append(names, k)
		}
		slices.Sort(names)
		return strings.Join(names, " ")
	}
	cases := []struct {
		terms []string
		want  string
	}{
		{nil, "ast-grep fzf fzf@14 rg"},
		// the start of a word of the key, plain or pinned
		{[]string{"fz"}, "fzf fzf@14"},
		{[]string{"grep"}, "ast-grep"},
		{[]string{"14"}, "fzf@14"},
		// or of an alias the registry gives the package
		{[]string{"rip"}, "rg"},
		{[]string{"ripgrep"}, "rg"},
		// each term is its own search
		{[]string{"rg", "ast"}, "ast-grep rg"},
		// the middle or the end of a word is not its start
		{[]string{"zf"}, ""},
		{[]string{"pgrep"}, ""},
	}
	for _, c := range cases {
		if got := keys(c.terms...); got != c.want {
			t.Errorf("findExtracts(%v) = %q, want %q", c.terms, got, c.want)
		}
	}
}

func TestRepoFound(t *testing.T) {
	entry := config.RepoEntry{
		URI:     "github.com/BurntSushi/ripgrep",
		Descr:   "Line-oriented regex search tool.",
		Aliases: []string{"ripgrep"},
	}
	cases := []struct {
		term string
		want bool
	}{
		{"rg", true},       // the name
		{"r", true},        // the start of it
		{"rip", true},      // an alias
		{"burnt", true},    // the owner
		{"regex", true},    // a word of the description
		{"oriented", true}, // behind a separator
		{"grep", false},    // the end of a word
		{"github", false},  // every entry lives there, so the host is not searched
		{"com", false},
		{"nosuch", false},
	}
	for _, c := range cases {
		if got := repoFound("rg", entry, c.term); got != c.want {
			t.Errorf("repoFound(%q) = %v, want %v", c.term, got, c.want)
		}
	}
}

func TestRunList_NameFilter(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	writeManifest(t, &config.Manifest{
		Repos: map[string]string{
			"fzf": "github.com/junegunn/fzf",
			"rg":  "github.com/BurntSushi/ripgrep",
		},
		Extracts: map[string]config.PackageEntry{
			"fzf": {Version: "0.58.0", Pin: "latest", Assets: []string{"fzf.tar.gz"}, Bin: map[string]string{"fzf": "fzf"}},
			"rg":  {Version: "14.1.0", Pin: "latest", Assets: []string{"rg.tar.gz"}, Bin: map[string]string{"rg": "rg"}},
		},
	})
	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	if err := runList(nil, []string{"fzf"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "fzf") {
		t.Errorf("expected fzf in filtered output:\n%s", out)
	}
	if strings.Contains(out, "ripgrep") || strings.Contains(out, "14.1.0") {
		t.Errorf("rg should be filtered out:\n%s", out)
	}
}

// TestRunList_ShowsUriTypeAndTarget guards list's table columns: asset and
// artifact were tried and dropped (asset can't be shown correctly per-row for
// a multi-asset package — bins/fonts are discovered from the combined overlay
// tree with no per-asset attribution kept). uri/type/target remain: where
// the package comes from, what kind of thing this is, and the name the user
// actually invokes it by.
func TestRunList_ShowsUriTypeAndTarget(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	writeManifest(t, &config.Manifest{
		Repos: map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{
			"fzf": {Version: "0.58.0", Pin: "latest", Assets: []string{"fzf-0.58.0-linux_amd64.tar.gz"}, Bin: map[string]string{"fzf": "bin/fzf"}},
		},
	})
	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	if err := runList(nil, nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "uri") || !strings.Contains(out, "type") || !strings.Contains(out, "target") {
		t.Errorf("expected \"uri\"/\"type\"/\"target\" column headers:\n%s", out)
	}
	if !strings.Contains(out, "bin") || !strings.Contains(out, "fzf") || !strings.Contains(out, "junegunn") {
		t.Errorf("expected the bin's type, shim name, and uri in the table:\n%s", out)
	}
	if strings.Contains(out, "fzf-0.58.0-linux_amd64.tar.gz") || strings.Contains(out, "bin/fzf") {
		t.Errorf("asset/artifact should not appear in the table:\n%s", out)
	}
}

func TestRunList_NoMatchVsEmpty(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})

	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	// Nothing installed at all.
	if err := runList(nil, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no packages installed") {
		t.Errorf("empty install set should say 'no packages installed':\n%s", buf.String())
	}

	// Something installed, but the filter matches nothing.
	writeManifest(t, &config.Manifest{
		Repos: map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{
			"fzf": {Version: "0.58.0", Pin: "latest", Assets: []string{"fzf.tar.gz"}, Bin: map[string]string{"fzf": "fzf"}},
		},
	})
	buf.Reset()
	if err := runList(nil, []string{"nope"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no packages matched") {
		t.Errorf("unmatched filter should say 'no packages matched':\n%s", buf.String())
	}
}

func TestRunList_Empty(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	quiet = true
	defer func() { quiet = false }()

	if err := runList(nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRunList_WithPackages(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	writeManifest(t, &config.Manifest{
		Repos: map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{
			"fzf": {Version: "0.58.0", Pin: "latest", Assets: []string{"fzf.tar.gz"}, Bin: map[string]string{"fzf": "fzf"}},
		},
	})
	longNames = true
	defer func() { longNames = false }()

	if err := runList(nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRunList_LongNames(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	writeManifest(t, &config.Manifest{
		Repos: map[string]string{
			"fzf": "github.com/junegunn/fzf",
			"rg":  "github.com/BurntSushi/ripgrep",
		},
		Extracts: map[string]config.PackageEntry{
			"fzf": {Version: "0.58.0"},
			"rg":  {Version: "14.1.0"},
		},
	})
	longNames = true
	defer func() { longNames = false }()

	if err := runList(nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRunList_ShortNames(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	writeManifest(t, &config.Manifest{
		Repos: map[string]string{
			"fzf": "github.com/junegunn/fzf",
			"rg":  "github.com/BurntSushi/ripgrep",
		},
		Extracts: map[string]config.PackageEntry{
			"fzf": {Version: "0.58.0"},
			"rg":  {Version: "14.1.0"},
		},
	})
	shortNames = true
	defer func() { shortNames = false }()

	if err := runList(nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRunRefresh(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{
		RepoSources: []string{"github.com/meop/ghpm-config"},
	})
	quiet = true
	defer func() { quiet = false }()

	fakeGHBin(t, `case "$*" in
  *api*) echo '{"repos": {"fzf": "github.com/junegunn/fzf"}}' ;;
  *) echo '{}' ;;
esac`)

	err := runRefresh(nil, nil)
	if err != nil {
		t.Logf("refresh error (expected without real gh): %v", err)
	}
}
