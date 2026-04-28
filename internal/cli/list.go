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
	cfg, err := config.LoadSettings()
	if err != nil {
		return err
	}
	manifest, err := config.LoadManifest()
	if err != nil {
		return err
	}
	if len(manifest.Installs) == 0 {
		fmt.Println("no packages installed")
		return nil
	}

	keys := make([]string, 0, len(manifest.Installs))
	for k := range manifest.Installs {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	rows := make([][]string, len(keys))
	for i, k := range keys {
		p := manifest.Installs[k]
		baseName, _, _ := config.ParseVersionSuffix(k)
		rows[i] = []string{k, p.Pin, p.Version, p.Asset, manifest.Repos[baseName]}
	}
	colors := []func(string) string{nil, nil, colorfn(cfg, "info"), nil, nil}
	printTable([]string{"name", "pin", "version", "asset", "repo"}, rows, colors)
	return nil
}
