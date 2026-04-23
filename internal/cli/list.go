package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List installed packages",
		Args:  cobra.NoArgs,
		RunE:  runList,
	}
}

func runList(cmd *cobra.Command, args []string) error {
	manifest, err := config.LoadManifest()
	if err != nil {
		return err
	}
	if len(manifest.Packages) == 0 {
		fmt.Println("No packages installed.")
		return nil
	}

	keys := make([]string, 0, len(manifest.Packages))
	for k := range manifest.Packages {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	fmt.Printf("%-30s %-15s %-10s %s\n", "NAME", "VERSION", "PINNED", "SOURCE")
	fmt.Printf("%-30s %-15s %-10s %s\n", "----", "-------", "------", "------")
	for _, k := range keys {
		p := manifest.Packages[k]
		pinned := "no"
		if p.Versioned {
			pinned = "yes"
		}
		fmt.Printf("%-30s %-15s %-10s %s\n", k, p.Version, pinned, p.Source)
	}
	return nil
}
