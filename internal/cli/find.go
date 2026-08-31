package cli

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

func newFindCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "find [term...]",
		Aliases: []string{"f", "fi", "se", "search"},
		Short:   "List or search cached repos by name or source",
		RunE:    runFind,
	}
	addNameFormatFlags(cmd)
	return cmd
}

type repoMatch struct {
	name   string
	source string
	descr  string
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
			all = append(all, repoMatch{name: name, source: entry.URI, descr: entry.Descr})
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

		lower := strings.ToLower(term)
		var matches []repoMatch
		for name, entry := range repos {
			if strings.Contains(strings.ToLower(name), lower) ||
				strings.Contains(strings.ToLower(entry.URI), lower) ||
				strings.Contains(strings.ToLower(entry.Descr), lower) {
				matches = append(matches, repoMatch{name: name, source: entry.URI, descr: entry.Descr})
			}
		}

		if len(matches) == 0 {
			print("no repos matching %q", term)
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

// printMatchTable renders the repo table, dropping the descr column entirely
// when no matched entry has one — a registry predating descr, or a user's own
// repo.toml written in the legacy uri-only form, should not print a column of
// blanks.
func printMatchTable(matches []repoMatch) {
	headers := []string{"name", "uri"}
	withDescr := slices.ContainsFunc(matches, func(m repoMatch) bool { return m.descr != "" })
	if withDescr {
		headers = append(headers, "descr")
	}
	rows := make([][]string, len(matches))
	for i, m := range matches {
		rows[i] = []string{m.name, m.source}
		if withDescr {
			rows[i] = append(rows[i], m.descr)
		}
	}
	printTable(headers, rows, nil)
}
