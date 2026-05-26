package cli

import (
	"fmt"
	"slices"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
)

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List installed packages",
		Args:    cobra.NoArgs,
		RunE:    runList,
	}
	cmd.Flags().BoolVarP(&onlyNames, "only-names", "o", false, "Print names only, one per line")
	return cmd
}

func runList(cmd *cobra.Command, args []string) error {
	ci, err := initCommand(cmdOptions{Manifest: true})
	if err != nil {
		return err
	}
	cfg := ci.cfg
	manifest := ci.manifest
	if len(manifest.Extracts) == 0 {
		printInfo(cfg, "no packages installed")
		return nil
	}

	keys := make([]string, 0, len(manifest.Extracts))
	for k := range manifest.Extracts {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	if onlyNames {
		for _, k := range keys {
			fmt.Println(k)
		}
		return nil
	}
	rows := make([][]string, len(keys))
	for i, k := range keys {
		p := manifest.Extracts[k]
		baseName, _, _ := config.ParseVersionSuffix(k)
		rows[i] = []string{k, p.Version, p.Pin, manifest.Repos[baseName], p.AssetName}
	}
	colors := []func(string) string{nil, colorfn(cfg, "info"), nil, nil, nil}
	printTable([]string{"name", "version", "pin", "repo", "asset"}, rows, colors)
	return nil
}
