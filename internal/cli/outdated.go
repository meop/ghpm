package cli

import (
	"cmp"
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
	addNameFormatFlags(cmd)
	return cmd
}

func runOutdated(cmd *cobra.Command, args []string) error {
	ci, err := initCommand(cmdOptions{Manifest: true, GH: true})
	if err != nil {
		return err
	}
	cfg := ci.cfg
	manifest := ci.manifest
	ghClient := ci.gh
	ctx := cmd.Context()

	type outdatedPkg struct {
		key       string
		installed string
		latest    string
		pkg       config.PackageEntry
		source    string
	}

	items := buildBatchItems(manifest.Extracts, manifest.Repos)

	if len(items) == 0 {
		print(msgAllUpToDate)
		return nil
	}

	results := ghClient.BatchLatestVersions(ctx, items, cfg.CacheTTL)

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
				printRateLimited(cfg, res.Key)
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
			pkgName, _, _ := config.ParseVersionSuffix(res.Key)
			outdated = append(outdated, outdatedPkg{
				key:       res.Key,
				installed: pkg.Version,
				latest:    latest,
				pkg:       pkg,
				source:    manifest.Repos[pkgName],
			})
		}
	}

	if rateLimited {
		printRateLimitSummary(cfg, checked, len(items), skipped)
	}

	if len(outdated) == 0 {
		if !rateLimited {
			print(msgAllUpToDate)
		}
		if hadErrors {
			return errSilent
		}
		return nil
	}

	slices.SortFunc(outdated, func(a, b outdatedPkg) int {
		return cmp.Compare(a.key, b.key)
	})

	keys := make([]string, len(outdated))
	for i, o := range outdated {
		keys[i] = o.key
	}
	if printNameList(keys) {
		if hadErrors {
			return errSilent
		}
		return nil
	}

	var tableRows [][]string
	for _, o := range outdated {
		assetNames := make([]string, 0, len(o.pkg.Asset))
		for a := range o.pkg.Asset {
			assetNames = append(assetNames, a)
		}
		slices.Sort(assetNames)
		for _, assetName := range assetNames {
			prefix := []string{o.key, o.installed, o.latest, o.pkg.Pin, o.source, assetName}
			tableRows = appendAssetEntryRows(tableRows, prefix, o.pkg.Asset[assetName])
		}
	}
	colors := []func(string) string{nil, colorfn(cfg, "old"), colorfn(cfg, "new"), nil, nil, nil, nil, nil, nil}
	printTable([]string{"name", "version", "update", "pin", "repo", "asset", "type", "artifact", "path"}, tableRows, colors)

	if hadErrors {
		return errSilent
	}
	return nil
}
