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
		Aliases: []string{"out", "ou", "stale"},
		Short:   "List packages with newer releases available",
		Args:    cobra.NoArgs,
		RunE:    runOutdated,
	}
	cmd.Flags().BoolVarP(&onlyNames, "only-names", "o", false, "Print names only, one per line")
	return cmd
}

func runOutdated(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadSettings()
	if err != nil {
		printFail(nil, "could not load settings: %v", err)
		return errSilent
	}
	manifest, err := config.LoadManifest()
	if err != nil {
		printFail(cfg, "could not load manifest: %v", err)
		return errSilent
	}
	if err := gh.CheckInstalled(); err != nil {
		printFail(cfg, "%v", err)
		return errSilent
	}

	type outdatedResult struct {
		name      string
		installed string
		latest    string
		pin       string
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
		printInfo(cfg, "all packages are up to date")
		return nil
	}

	results := gh.BatchLatestVersions(items, cfg.CacheTTL)

	var outdated []outdatedResult
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
			outdated = append(outdated, outdatedResult{
				name:      res.Key,
				installed: pkg.Version,
				latest:    latest,
				pin:       pkg.Pin,
				source:    manifest.Repos[name],
			})
		}
	}

	if rateLimited && skipped > 0 {
		printWarn(cfg, "checked %d/%d packages (%d skipped due to rate limiting)", checked, len(items), skipped)
	}

	if len(outdated) == 0 {
		if !rateLimited {
			printInfo(cfg, "all packages are up to date")
		}
		if hadErrors {
			return errSilent
		}
		return nil
	}

	slices.SortFunc(outdated, func(a, b outdatedResult) int {
		return cmp.Compare(a.name, b.name)
	})

	if onlyNames {
		for _, o := range outdated {
			fmt.Println(o.name)
		}
		if hadErrors {
			return errSilent
		}
		return nil
	}
	rows := make([][]string, len(outdated))
	for i, o := range outdated {
		rows[i] = []string{o.name, o.installed, o.latest, o.pin, o.source, ""}
	}
	colors := []func(string) string{nil, colorfn(cfg, "old"), colorfn(cfg, "new"), nil, nil, nil}
	printTable([]string{"name", "version", "update", "pin", "repo", "asset"}, rows, colors)
	if hadErrors {
		return errSilent
	}
	return nil
}
