package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
)

func newInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "info <name> [name...]",
		Aliases: []string{"in", "show"},
		Short:   "Show releases and available assets for packages",
		Args:    cobra.MinimumNArgs(1),
		RunE:    runInfo,
	}
}

func runInfo(cmd *cobra.Command, args []string) error {
	ci, err := initCommand(cmdOptions{Manifest: true, GH: true, Repos: true})
	if err != nil {
		return err
	}
	cfg := ci.cfg
	manifest := ci.manifest
	repos := ci.repos
	ctx := cmd.Context()

	var hadErrors bool
	for _, arg := range args {
		name, ver, _ := config.ParseVersionSuffix(arg)
		fmt.Printf("info: %s\n", name)
		if err := config.ValidateName(name); err != nil {
			printFail(cfg, "%s: %v", arg, err)
			hadErrors = true
			continue
		}
		source, err := config.ResolveSource(name, ver, manifest, repos)
		if err != nil {
			printFail(cfg, "%s: %v", arg, err)
			hadErrors = true
			continue
		}
		owner, repo, err := gh.SplitSource(source)
		if err != nil {
			printFail(cfg, "%s: %v", arg, err)
			hadErrors = true
			continue
		}

		fmt.Printf("\n%s (%s)\n", arg, source)
		fmt.Println(repeatStr("─", 60))

		if ver != "" {
			rel, err := gh.GetReleaseByTag(ctx, owner, repo, ver)
			if err != nil {
				printFail(cfg, "%v", err)
				hadErrors = true
				continue
			}
			printReleaseInfo(rel)
		} else {
			releases, err := gh.ListReleases(ctx, owner, repo)
			if err != nil {
				printFail(cfg, "%v", err)
				hadErrors = true
				continue
			}
			limit := min(len(releases), 10)
			fmt.Printf("  recent releases (%d shown):\n", limit)
			for _, r := range releases[:limit] {
				fmt.Printf("    %s\n", config.NormalizeVersion(r.TagName))
			}
			if len(releases) > 0 {
				rel, err := gh.GetLatestRelease(ctx, owner, repo)
				if err == nil {
					fmt.Println()
					printReleaseInfo(rel)
				}
			}
		}
	}
	if hadErrors {
		return errSilent
	}
	return nil
}

func printReleaseInfo(rel gh.Release) {
	fmt.Printf("  tag: %s\n  assets:\n", config.NormalizeVersion(rel.TagName))
	for _, a := range rel.Assets {
		fmt.Printf("    %-60s %d bytes\n", a.Name, a.Size)
	}
}

func repeatStr(s string, n int) string {
	return strings.Repeat(s, n)
}
