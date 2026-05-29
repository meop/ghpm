package cli

import (
	"fmt"
	"slices"
	"strings"

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
	cmd.Flags().BoolVarP(&longNames, "long-names", "l", false, "Print names only, one per line")
	cmd.Flags().BoolVarP(&shortNames, "short-names", "s", false, "Print names only, space-separated on one line")
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

	if longNames {
		for _, k := range keys {
			fmt.Println(k)
		}
		return nil
	}
	if shortNames {
		fmt.Println(strings.Join(keys, " "))
		return nil
	}

	type row struct {
		name, version, pin, repo, asset, typ, artifact, path string
	}
	var rows []row
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
			ae := p.Asset[assetName]
			if len(ae.Bin) > 0 {
				shimNames := make([]string, 0, len(ae.Bin))
				for s := range ae.Bin {
					shimNames = append(shimNames, s)
				}
				slices.Sort(shimNames)
				for _, shimName := range shimNames {
					rows = append(rows, row{k, p.Version, p.Pin, repo, assetName, "bin", shimName, ae.Bin[shimName]})
				}
			}
			if len(ae.Font) > 0 {
				fontNames := make([]string, 0, len(ae.Font))
				for f := range ae.Font {
					fontNames = append(fontNames, f)
				}
				slices.Sort(fontNames)
				for _, fontName := range fontNames {
					rows = append(rows, row{k, p.Version, p.Pin, repo, assetName, "font", fontName, ae.Font[fontName]})
				}
			}
		}
	}

	if len(rows) == 0 {
		print("no packages installed")
		return nil
	}

	tableRows := make([][]string, len(rows))
	for i, r := range rows {
		tableRows[i] = []string{r.name, r.version, r.pin, r.repo, r.asset, r.typ, r.artifact, r.path}
	}
	colors := []func(string) string{nil, colorfn(cfg, "info"), nil, nil, nil, nil, nil, nil}
	printTable([]string{"name", "version", "pin", "repo", "asset", "type", "artifact", "path"}, tableRows, colors)
	return nil
}
