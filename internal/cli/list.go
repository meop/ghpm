package cli

import (
	"fmt"
	"slices"

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
	if len(manifest.Installs) == 0 {
		fmt.Println("No packages installed.")
		return nil
	}

	keys := make([]string, 0, len(manifest.Installs))
	for k := range manifest.Installs {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	fmt.Printf("%-30s %-15s %-10s %s\n", "NAME", "VERSION", "PIN", "SOURCE")
	fmt.Printf("%-30s %-15s %-10s %s\n", "----", "-------", "---", "------")
	for _, k := range keys {
		p := manifest.Installs[k]
		baseName, _, _ := config.ParseVersionSuffix(k)
		src := manifest.Repos[baseName]
		fmt.Printf("%-30s %-15s %-10s %s\n", k, p.Version, p.Pin, src)
	}
	return nil
}
