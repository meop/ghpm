package cli

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/parallel"
)

func newOutdatedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "outdated",
		Short: "Show packages with newer releases available",
		Args:  cobra.NoArgs,
		RunE:  runOutdated,
	}
}

func runOutdated(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadSettings()
	if err != nil {
		return err
	}
	manifest, err := config.LoadManifest()
	if err != nil {
		return err
	}
	if err := gh.CheckInstalled(); err != nil {
		return err
	}

	type outdatedResult struct {
		name      string
		installed string
		latest    string
	}

	tasks := make([]parallel.Task, 0)
	for key, pkg := range manifest.Packages {
		_, verStr, isPinned := config.ParseVersionSuffix(key)
		var c config.Constraint
		if isPinned {
			parsed, cerr := config.ParseConstraint(verStr)
			if cerr != nil || parsed.Level == config.PinExact {
				continue
			}
			c = parsed
		}
		tasks = append(tasks, parallel.Task{
			Name: key,
			Run: func() (any, error) {
				owner, repo, err := gh.SplitSource(pkg.Source)
				if err != nil {
					return nil, err
				}
				var rel gh.Release
				if isPinned {
					rel, err = gh.FindLatestMatching(owner, repo, c)
				} else {
					rel, err = gh.GetLatestRelease(owner, repo)
				}
				if err != nil {
					return nil, err
				}
				latest := config.NormalizeVersion(rel.TagName)
				if config.CompareVersions(latest, pkg.Version) > 0 {
					return outdatedResult{name: key, installed: pkg.Version, latest: latest}, nil
				}
				return nil, nil
			},
		})
	}

	results := parallel.Run(cmd.Context(), tasks, cfg.Parallelism)
	var outdated []outdatedResult
	for _, res := range results {
		if res.Err != nil {
			fmt.Printf("✗ %s: %v\n", res.Name, res.Err)
			continue
		}
		if res.Value == nil {
			continue
		}
		r, ok := res.Value.(outdatedResult)
		if !ok {
			continue
		}
		outdated = append(outdated, r)
	}

	if len(outdated) == 0 {
		fmt.Println("All packages are up to date.")
		return nil
	}

	slices.SortFunc(outdated, func(a, b outdatedResult) int {
		return cmp.Compare(a.name, b.name)
	})
	fmt.Printf("%-30s %-15s %s\n", "NAME", "INSTALLED", "LATEST")
	fmt.Printf("%-30s %-15s %s\n", "----", "---------", "------")
	for _, o := range outdated {
		fmt.Printf("%-30s %-15s %s\n", o.name, o.installed, o.latest)
	}
	return nil
}
