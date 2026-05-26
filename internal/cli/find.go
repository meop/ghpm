package cli

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

func newFindCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "find [term...]",
		Aliases: []string{"fi", "se", "search"},
		Short:   "List or search cached repos by name or source",
		RunE:    runFind,
	}
	cmd.Flags().BoolVarP(&onlyNames, "only-names", "o", false, "Print names only, one per line")
	return cmd
}

type repoMatch struct {
	name   string
	source string
}

func runFind(cmd *cobra.Command, args []string) error {
	ci, err := initCommand(cmdOptions{Repos: true})
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
		for name, source := range repos {
			all = append(all, repoMatch{name: name, source: source})
		}
		slices.SortFunc(all, func(a, b repoMatch) int {
			return cmp.Compare(a.name, b.name)
		})
		if onlyNames {
			for _, m := range all {
				fmt.Println(m.name)
			}
			return nil
		}
		rows := make([][]string, len(all))
		for i, m := range all {
			rows[i] = []string{m.name, m.source}
		}
		printTable([]string{"name", "repo"}, rows, nil)
		return nil
	}

	for i, term := range args {
		if i > 0 {
			fmt.Println()
		}
		if len(args) > 1 {
			fmt.Printf("find: %s\n", term)
		}

		lower := strings.ToLower(term)
		var matches []repoMatch
		for name, source := range repos {
			if strings.Contains(strings.ToLower(name), lower) ||
				strings.Contains(strings.ToLower(source), lower) {
				matches = append(matches, repoMatch{name: name, source: source})
			}
		}

		if len(matches) == 0 {
			print("no repos matching %q", term)
			continue
		}

		slices.SortFunc(matches, func(a, b repoMatch) int {
			return cmp.Compare(a.name, b.name)
		})

		if onlyNames {
			for _, m := range matches {
				fmt.Println(m.name)
			}
			continue
		}
		rows := make([][]string, len(matches))
		for i, m := range matches {
			rows[i] = []string{m.name, m.source}
		}
		printTable([]string{"name", "repo"}, rows, nil)
	}
	return nil
}
