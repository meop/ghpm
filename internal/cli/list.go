package cli

import (
	"slices"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
)

var longNames, shortNames bool

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"li", "ls"},
		Short:   "List installed packages",
		Args:    cobra.NoArgs,
		RunE:    runList,
	}
	addNameFormatFlags(cmd)
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
		print("no packages installed")
		return nil
	}

	keys := make([]string, 0, len(manifest.Extracts))
	for k := range manifest.Extracts {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	if printNameList(keys) {
		return nil
	}

	var tableRows [][]string
	for _, k := range keys {
		p := manifest.Extracts[k]
		baseName, _, _ := config.ParseVersionSuffix(k)
		repo := manifest.Repos[baseName]
		assetNames := make([]string, 0, len(p.Asset))
		for a := range p.Asset {
			assetNames = append(assetNames, a)
		}
		slices.Sort(assetNames)
		for _, assetName := range assetNames {
			prefix := []string{k, p.Version, p.Pin, repo, assetName}
			tableRows = appendAssetEntryRows(tableRows, prefix, p.Asset[assetName])
		}
	}

	if len(tableRows) == 0 {
		print("no packages installed")
		return nil
	}
	colors := []func(string) string{nil, colorfn(cfg, "info"), nil, nil, nil, nil, nil, nil}
	printTable([]string{"name", "version", "pin", "repo", "asset", "artifact", "type", "target"}, tableRows, colors)
	return nil
}
