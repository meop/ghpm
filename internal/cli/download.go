package cli

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/parallel"
	"github.com/meop/ghpm/internal/asset"
	"github.com/meop/ghpm/internal/store"
)

func newDownloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download <name> [name...]",
		Short: "Download release assets without extracting",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runDownload,
	}
	cmd.Flags().String("path", "", "Destination directory (default: ~/.ghpm/release/)")
	return cmd
}

func runDownload(cmd *cobra.Command, args []string) error {
	destPath, _ := cmd.Flags().GetString("path")
	cfg, err := config.LoadSettings()
	if err != nil {
		return err
	}
	if cfg.NoVerify {
		NoVerify = true
	}
	manifest, err := config.LoadManifest()
	if err != nil {
		return err
	}
	if err := gh.CheckInstalled(); err != nil {
		return err
	}
	aliases, aliasErr := config.LoadAliases()
	if aliasErr != nil {
		color.Yellow("⚠ could not load aliases: %v", aliasErr)
	}

	type dlJob struct {
		name    string
		source  string
		version string
		pinned  bool
		release gh.Release
		chosen  gh.Asset
	}

	tasks := make([]parallel.Task, 0, len(args))
	for _, arg := range args {
		name, ver, pinned := config.ParseVersionSuffix(arg)
		if err := config.ValidateName(name); err != nil {
			color.Red("✗ %s: %v", arg, err)
			continue
		}
		source, err := config.ResolveSource(name, ver, manifest, aliases)
		if err != nil {
			color.Red("✗ %s: %v", arg, err)
			continue
		}
		tasks = append(tasks, parallel.Task{
			Name: name,
			Run: func() (any, error) {
				owner, repo, err := gh.SplitSource(source)
				if err != nil {
					return nil, err
				}
				var rel gh.Release
				if ver != "" {
					rel, err = gh.GetReleaseByTag(owner, repo, ver)
				} else {
					rel, err = gh.GetLatestRelease(owner, repo)
				}
				if err != nil {
					return nil, err
				}
				var hint string
				if existing, ok := manifest.Packages[name]; ok {
					hint = existing.AssetPattern
				}
				chosen, err := asset.SelectAsset(rel.Assets, cfg, hint)
				if err != nil {
					return nil, err
				}
				return dlJob{name: name, source: source, version: ver, pinned: pinned, release: rel, chosen: chosen}, nil
			},
		})
	}

	results := parallel.Run(cmd.Context(), tasks, cfg.Parallelism)
	var ready []dlJob
	for _, res := range results {
		if res.Err != nil {
			color.Red("✗ %s: %v", res.Name, res.Err)
			continue
		}
		ready = append(ready, res.Value.(dlJob))
	}
	if len(ready) == 0 {
		return nil
	}

	if DryRun {
		for _, r := range ready {
			fmt.Printf("[dry-run] would download %s %s (asset: %s)\n", r.name, r.release.TagName, r.chosen.Name)
		}
		return nil
	}

	if !promptConfirm(fmt.Sprintf("Download %d asset(s)?", len(ready))) {
		fmt.Println("Aborted.")
		return nil
	}

	dlTasks := make([]parallel.Task, len(ready))
	for i, r := range ready {
		dlTasks[i] = parallel.Task{
			Name: r.name,
			Run: func() (any, error) {
				owner, repo, _ := gh.SplitSource(r.source)
				dest := destPath
				if dest == "" {
					var err error
					dest, err = store.ReleaseDir(r.source, r.release.TagName)
					if err != nil {
						return nil, err
					}
				}
				return nil, gh.DownloadAsset(owner, repo, r.release.TagName, r.chosen.Name, dest)
			},
		}
	}

	for _, res := range parallel.Run(cmd.Context(), dlTasks, cfg.Parallelism) {
		if res.Err != nil {
			color.Red("✗ %s: %v", res.Name, res.Err)
		} else {
			color.Green("✓ downloaded %s", res.Name)
		}
	}
	return nil
}
