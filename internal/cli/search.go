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
		Short: "Search cached tools by name or source",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runSearch,
	}
}

type toolMatch struct {
	name   string
	source string
}

func runSearch(cmd *cobra.Command, args []string) error {
	tools, err := config.LoadTools()
	if err != nil {
		return err
	}
	if len(tools) == 0 {
		fmt.Println("No tools cached — run 'ghpm update' to fetch them.")
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
		var matches []toolMatch
		for name, source := range tools {
			if strings.Contains(strings.ToLower(name), lower) ||
				strings.Contains(strings.ToLower(source), lower) {
				matches = append(matches, toolMatch{name: name, source: source})
			}
		}

		if len(matches) == 0 {
			fmt.Printf("no tools matching %q\n", term)
			continue
		}

		slices.SortFunc(matches, func(a, b toolMatch) int {
			return cmp.Compare(a.name, b.name)
		})

		fmt.Printf("%-25s %s\n", "NAME", "SOURCE")
		fmt.Printf("%-25s %s\n", "----", "------")
		for _, m := range matches {
			fmt.Printf("%-25s %s\n", m.name, m.source)
		}
	}
	return nil
}
