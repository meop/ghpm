package cli

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/golie/ui"
)

func newFindCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "find [term...]",
		Aliases: []string{"f", "fi", "se", "search"},
		Short:   "List or search cached repos by name, alias, source or description",
		RunE:    runFind,
	}
	addNameFormatFlags(cmd)
	return cmd
}

type repoMatch struct {
	name    string
	aliases []string
	source  string
	descr   string
}

func newRepoMatch(name string, entry config.RepoEntry) repoMatch {
	return repoMatch{name: name, aliases: entry.Aliases, source: entry.URI, descr: entry.Descr}
}

// repoFound is find's question, and the widest ghpm asks: the term starts a
// word of the entry's name, of an alias, of its owner/repo, or of its
// description (config.WordStartsWith). find is where a half remembered name —
// or only what the tool does, or whose it is — becomes the name to type, so it
// reads everything the registry says about an entry. The commands that act
// never do: they take a name or an alias whole.
func repoFound(name string, entry config.RepoEntry, term string) bool {
	if config.WordStartsWith(name, term) ||
		config.WordStartsWith(strings.TrimPrefix(entry.URI, "github.com/"), term) ||
		config.WordStartsWith(entry.Descr, term) {
		return true
	}
	return slices.ContainsFunc(entry.Aliases, func(alias string) bool {
		return config.WordStartsWith(alias, term)
	})
}

func runFind(cmd *cobra.Command, args []string) error {
	ci, err := initCommand(context.Background(), cmdOptions{Repos: true})
	if err != nil {
		return err
	}
	repos := ci.repos
	if len(repos) == 0 {
		print("no repos cached")
		return nil
	}

	if len(args) == 0 {
		var all []repoMatch
		for name, entry := range repos {
			all = append(all, newRepoMatch(name, entry))
		}
		slices.SortFunc(all, func(a, b repoMatch) int {
			return cmp.Compare(a.name, b.name)
		})
		names := make([]string, len(all))
		for i, m := range all {
			names[i] = m.name
		}
		if printNameList(names) {
			return nil
		}
		printMatchTable(all)
		return nil
	}

	for _, term := range args {
		sep()
		if len(args) > 1 {
			print("find: %s", term)
		}

		var matches []repoMatch
		for name, entry := range repos {
			if repoFound(name, entry, term) {
				matches = append(matches, newRepoMatch(name, entry))
			}
		}

		if len(matches) == 0 {
			print("no repos matching %q", term)
			findOnGitHub(term)
			continue
		}

		slices.SortFunc(matches, func(a, b repoMatch) int {
			return cmp.Compare(a.name, b.name)
		})

		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.name
		}
		if printNameList(names) {
			continue
		}
		printMatchTable(matches)
	}
	return nil
}

// findOnGitHub is find's deeper search, and only ever a fallback: GitHub is
// asked about a term nothing in the registry was found by, and only once the
// user says so, since it is the one part of find that leaves the machine. With
// nobody to ask, or with output shaped for a script, there is no fallback.
func findOnGitHub(term string) {
	if dryRun || longNames || shortNames || (!yes && !ui.Interactive()) {
		return
	}
	if !promptConfirm(fmt.Sprintf("search github for %q?", term)) {
		return
	}
	if err := gh.CheckInstalled(context.Background()); err != nil {
		printFail(nil, "%v", err)
		return
	}
	hits, err := config.SearchRepos(term)
	if err != nil {
		printFail(nil, "%v", err)
		return
	}
	rows := make([][]string, len(hits))
	for i, h := range hits {
		rows[i] = []string{"github.com/" + h.FullName, h.Descr}
	}
	printTable([]string{"uri", "descr"}, rows, nil)
}

// printMatchTable renders the repo table, dropping the aliases and descr
// columns entirely when no matched entry has one — a registry predating either,
// or a user's own repo.toml written in the legacy uri-only form, should not
// print a column of blanks.
func printMatchTable(matches []repoMatch) {
	withAliases := slices.ContainsFunc(matches, func(m repoMatch) bool { return len(m.aliases) > 0 })
	withDescr := slices.ContainsFunc(matches, func(m repoMatch) bool { return m.descr != "" })
	headers := []string{"name"}
	if withAliases {
		headers = append(headers, "aliases")
	}
	headers = append(headers, "uri")
	if withDescr {
		headers = append(headers, "descr")
	}
	rows := make([][]string, len(matches))
	for i, m := range matches {
		rows[i] = []string{m.name}
		if withAliases {
			rows[i] = append(rows[i], strings.Join(m.aliases, ", "))
		}
		rows[i] = append(rows[i], m.source)
		if withDescr {
			rows[i] = append(rows[i], m.descr)
		}
	}
	printTable(headers, rows, nil)
}
