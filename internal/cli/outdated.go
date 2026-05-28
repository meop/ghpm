package cli

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
)

func newOutdatedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "outdated",
		Aliases: []string{"ou", "out", "stale"},
		Short:   "List packages with newer releases available",
		Args:    cobra.NoArgs,
		RunE:    runOutdated,
	}
	cmd.Flags().BoolVarP(&onlyNames, "only-names", "o", false, "Print names only, one per line")
	return cmd
}

func runOutdated(cmd *cobra.Command, args []string) error {
	ci, err := initCommand(cmdOptions{Manifest: true, GH: true})
	if err != nil {
		return err
	}
	cfg := ci.cfg
	manifest := ci.manifest
	ctx := cmd.Context()

	type outdatedPkg struct {
		key       string
		installed string
		latest    string
		pkg       config.PackageEntry
		source    string
	}

	items := make([]gh.BatchItem, 0)
	for key, pkg := range manifest.Extracts {
		if pkg.Pin == "fixed" {
			continue
		}
		name, verStr, isPinned := config.ParseVersionSuffix(key)
		source := manifest.Repos[name]
		var c config.Constraint
		if isPinned {
			parsed, cerr := config.ParseConstraint(verStr)
			if cerr != nil {
				continue
			}
			c = parsed
		}
		items = append(items, gh.BatchItem{
			Key:    key,
			Source: source,
			Pin:    c,
		})
	}

	if len(items) == 0 {
		print("all packages are up to date")
		return nil
	}

	results := gh.BatchLatestVersions(ctx, items, cfg.CacheTTL)

	var outdated []outdatedPkg
	checked := 0
	skipped := 0
	rateLimited := false
	var hadErrors bool

	for _, res := range results {
		if res.Err != nil {
			if gh.IsRateLimited(res.Err) {
				rateLimited = true
				skipped++
				printWarn(cfg, "%s: rate limited", res.Key)
				continue
			}
			printFail(cfg, "%s: %v", res.Key, res.Err)
			hadErrors = true
			continue
		}
		checked++
		pkg := manifest.Extracts[res.Key]
		latest := config.NormalizeVersion(res.LatestTag)
		if config.CompareVersions(latest, pkg.Version) > 0 {
			name, _, _ := config.ParseVersionSuffix(res.Key)
			outdated = append(outdated, outdatedPkg{
				key:       res.Key,
				installed: pkg.Version,
				latest:    latest,
				pkg:       pkg,
				source:    manifest.Repos[name],
			})
		}
	}

	if rateLimited && skipped > 0 {
		printWarn(cfg, "checked %d/%d packages (%d skipped due to rate limiting)", checked, len(items), skipped)
	}

	if len(outdated) == 0 {
		if !rateLimited {
			print("all packages are up to date")
		}
		if hadErrors {
			return errSilent
		}
		return nil
	}

	slices.SortFunc(outdated, func(a, b outdatedPkg) int {
		return cmp.Compare(a.key, b.key)
	})

	if onlyNames {
		for _, o := range outdated {
			fmt.Println(o.key)
		}
		if hadErrors {
			return errSilent
		}
		return nil
	}

	type row struct {
		name, installed, latest, pin, repo, asset, typ, artifact, path string
	}
	var rows []row
	for _, o := range outdated {
		assetNames := make([]string, 0, len(o.pkg.Asset))
		for a := range o.pkg.Asset {
			assetNames = append(assetNames, a)
		}
		slices.Sort(assetNames)
		for _, assetName := range assetNames {
			ae := o.pkg.Asset[assetName]
			if len(ae.Bin) > 0 {
				shimNames := make([]string, 0, len(ae.Bin))
				for s := range ae.Bin {
					shimNames = append(shimNames, s)
				}
				slices.Sort(shimNames)
				for _, shimName := range shimNames {
					rows = append(rows, row{o.key, o.installed, o.latest, o.pkg.Pin, o.source, assetName, "bin", shimName, ae.Bin[shimName]})
				}
			}
			if len(ae.Font) > 0 {
				fontNames := make([]string, 0, len(ae.Font))
				for f := range ae.Font {
					fontNames = append(fontNames, f)
				}
				slices.Sort(fontNames)
				for _, fontName := range fontNames {
					rows = append(rows, row{o.key, o.installed, o.latest, o.pkg.Pin, o.source, assetName, "font", fontName, ae.Font[fontName]})
				}
			}
		}
	}

	tableRows := make([][]string, len(rows))
	for i, r := range rows {
		tableRows[i] = []string{r.name, r.installed, r.latest, r.pin, r.repo, r.asset, r.typ, r.artifact, r.path}
	}
	colors := []func(string) string{nil, colorfn(cfg, "old"), colorfn(cfg, "new"), nil, nil, nil, nil, nil, nil}
	printTable([]string{"name", "version", "update", "pin", "repo", "asset", "type", "artifact", "path"}, tableRows, colors)

	if hadErrors {
		return errSilent
	}
	return nil
}
