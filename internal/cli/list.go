package cli

import (
	"slices"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List installed packages",
		Args:    cobra.NoArgs,
		RunE:    runList,
	}
}

func runList(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadSettings()
	if err != nil {
		return err
	}
	manifest, err := config.LoadManifest()
	if err != nil {
		return err
	}
	if len(manifest.Extracts) == 0 {
		printInfo(cfg, "no packages installed")
		return nil
	}

	keys := make([]string, 0, len(manifest.Extracts))
	for k := range manifest.Extracts {
		keys = append(keys, k)
	}
	slices.Sort(keys)

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
