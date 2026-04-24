package cli

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
)

func newInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info <name> [name...]",
		Short: "Show release info and available assets for packages",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runInfo,
	}
}

func runInfo(cmd *cobra.Command, args []string) error {
	manifest, err := config.LoadManifest()
	if err != nil {
		return err
	}
	if err := gh.CheckInstalled(); err != nil {
		return err
	}
	tools, toolErr := config.LoadTools()
	if toolErr != nil {
		color.Yellow("⚠ could not load tools: %v", toolErr)
	}

	for _, arg := range args {
		name, ver, _ := config.ParseVersionSuffix(arg)
		if err := config.ValidateName(name); err != nil {
			fmt.Printf("✗ %s: %v\n", arg, err)
			continue
		}
		source, err := config.ResolveSource(name, ver, manifest, tools)
		if err != nil {
			fmt.Printf("✗ %s: %v\n", arg, err)
			continue
		}
		owner, repo, err := gh.SplitSource(source)
		if err != nil {
			fmt.Printf("✗ %s: %v\n", arg, err)
			continue
		}

		fmt.Printf("\n%s (%s)\n", arg, source)
		fmt.Println(repeatStr("─", 60))

		if ver != "" {
			rel, err := gh.GetReleaseByTag(owner, repo, ver)
			if err != nil {
				fmt.Printf("  error: %v\n", err)
				continue
			}
			printReleaseInfo(rel)
		} else {
			releases, err := gh.ListReleases(owner, repo)
			if err != nil {
				fmt.Printf("  error: %v\n", err)
				continue
			}
			limit := 10
			if len(releases) < limit {
				limit = len(releases)
			}
			fmt.Printf("  Recent releases (%d shown):\n", limit)
			for _, r := range releases[:limit] {
				latest := ""
				if r.IsLatest {
					latest = " (latest)"
				}
				fmt.Printf("    %s%s\n", r.TagName, latest)
			}
			if len(releases) > 0 {
				rel, err := gh.GetLatestRelease(owner, repo)
				if err == nil {
					fmt.Println()
					printReleaseInfo(rel)
				}
			}
		}
	}
	return nil
}

func printReleaseInfo(rel gh.Release) {
	fmt.Printf("  Tag: %s\n  Assets:\n", rel.TagName)
	for _, a := range rel.Assets {
		fmt.Printf("    %-60s %d bytes\n", a.Name, a.Size)
	}
}

func repeatStr(s string, n int) string {
	return strings.Repeat(s, n)
}
