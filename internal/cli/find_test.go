package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/store"
	"github.com/meop/ghpm/internal/ui"
)

func writeRepoTOML(t *testing.T, content string) {
	t.Helper()
	base, err := store.ReposBaseDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "test")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "repo.toml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

const aliasedRepos = `
[rg]
uri = "github.com/BurntSushi/ripgrep"
descr = "Line-oriented regex search tool."
aliases = ["ripgrep"]

[fzf]
uri = "github.com/junegunn/fzf"
descr = "Command-line fuzzy finder."
`

// find is the wide one: a term that starts a word of a name, an alias, an
// owner/repo or a description finds the entry, and the table shows the aliases
// so the names add will take are all in view.
func TestRunFind_WordStart(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	writeRepoTOML(t, aliasedRepos)
	ui.SetNonInteractive(true)
	t.Cleanup(func() { ui.SetNonInteractive(false) })

	find := func(term string) string {
		var buf bytes.Buffer
		ui.SetOutput(&buf)
		defer ui.SetOutput(os.Stdout)
		if err := runFind(nil, []string{term}); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}

	for _, term := range []string{"rg", "rip", "ripgrep", "burnt", "regex"} {
		out := find(term)
		if !strings.Contains(out, "github.com/BurntSushi/ripgrep") || strings.Contains(out, "junegunn") {
			t.Errorf("find %q should list rg alone:\n%s", term, out)
		}
		if !strings.Contains(out, "aliases") {
			t.Errorf("find %q should show the aliases column:\n%s", term, out)
		}
	}
	// the end of a word is not its start, and with nobody to ask there is no deeper search
	for _, term := range []string{"grep", "finder.x", "zf"} {
		out := find(term)
		if !strings.Contains(out, "no repos matching") {
			t.Errorf("find %q should match nothing:\n%s", term, out)
		}
	}
}

// remove acts, so it takes a name whole: the key, or an alias of it, and never
// the start of either.
func TestRunRemove_ByAlias(t *testing.T) {
	withHome(t)
	yes = true
	defer func() { yes = false }()
	writeRepoTOML(t, aliasedRepos)
	manifest := func() *config.Manifest {
		return &config.Manifest{
			Repos:    map[string]string{"rg": "github.com/BurntSushi/ripgrep"},
			Extracts: map[string]config.PackageEntry{"rg": {Version: "14.1.0"}},
		}
	}

	writeManifest(t, manifest())
	if err := runRemove(nil, []string{"rip"}); err != nil {
		t.Fatal(err)
	}
	m, err := config.LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Extracts["rg"]; !ok {
		t.Fatal("part of an alias must not remove the package")
	}

	if err := runRemove(nil, []string{"ripgrep"}); err != nil {
		t.Fatal(err)
	}
	m, err = config.LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Extracts["rg"]; ok {
		t.Fatal("an alias should remove the package it names")
	}
}

// list reads, so the start of a word of an alias is enough to show a package.
func TestRunList_ByAliasWordStart(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	writeRepoTOML(t, aliasedRepos)
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

	if err := runList(nil, []string{"rip"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "14.1.0") || strings.Contains(out, "0.58.0") {
		t.Errorf("list rip should show rg alone:\n%s", out)
	}
}
