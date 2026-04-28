package cli

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
)

func newSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <term> [term...]",
		Short: "Search cached repos by name or source",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runSearch,
	}
}

type repoMatch struct {
	name   string
	source string
}

func runSearch(cmd *cobra.Command, args []string) error {
	repos, err := config.LoadRepos()
	if err != nil {
		return err
	}
	if len(repos) == 0 {
		fmt.Println("no repos cached")
		return nil
	}

	for i, term := range args {
		if i > 0 {
			fmt.Println()
		}
		if len(args) > 1 {
			fmt.Printf("search: %s\n", term)
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
			fmt.Printf("no repos matching %q\n", term)
			continue
		}

		slices.SortFunc(matches, func(a, b repoMatch) int {
			return cmp.Compare(a.name, b.name)
		})

		rows := make([][]string, len(matches))
		for i, m := range matches {
			rows[i] = []string{m.name, m.source}
		}
		printTable([]string{"name", "repo"}, rows, nil)
	}
	return nil
}
