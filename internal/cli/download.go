package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/asset"
	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/parallel"
)

func newDownloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "download <name> [name...]",
		Aliases: []string{"down"},
		Short:   "Download release assets without extracting",
		Args:    cobra.MinimumNArgs(1),
		RunE:    runDownload,
	}
	cmd.Flags().String("path", "", "Destination directory (default: ~/.ghpm/download/)")
	addSkipHashCheckFlag(cmd)
	return cmd
}

func runDownload(cmd *cobra.Command, args []string) error {
	destPath, _ := cmd.Flags().GetString("path")
	ci, err := initCommand(cmdOptions{Manifest: true, GH: true, Repos: true, SkipHashCheck: true})
	if err != nil {
		return err
	}
	cfg := ci.cfg
	manifest := ci.manifest
	repos := ci.repos
	ghClient := ci.gh
	dirs := ci.dirs
	ctx := cmd.Context()

	type dlJob struct {
		name    string
		source  string
		version string
		pinned  bool
		release gh.Release
		chosen  gh.Asset
	}

	tasks := make([]parallel.Task[dlJob], 0, len(args))
	var hadErrors bool
	for _, arg := range args {
		name, ver, pinned := config.ParseVersionSuffix(arg)
		if err := config.ValidateName(name); err != nil {
			printFail(cfg, "%s: %v", name, err)
			hadErrors = true
			continue
		}
		source, err := config.ResolveSource(name, ver, manifest, repos)
		if err != nil {
			printFail(cfg, "%s: %v", name, err)
			hadErrors = true
			continue
		}
		tasks = append(tasks, parallel.Task[dlJob]{
			Name: name,
			Run: func() (dlJob, error) {
				owner, repo, err := gh.SplitSource(source)
				if err != nil {
					return dlJob{}, err
				}
				var rel gh.Release
				if ver != "" {
					rel, err = ghClient.GetReleaseByTag(ctx, owner, repo, ver)
				} else {
					rel, err = ghClient.GetLatestRelease(ctx, owner, repo)
				}
				if err != nil {
					return dlJob{}, err
				}
				ac, err := asset.SelectAssetAuto(rel.Assets, cfg, "", name)
				if err != nil {
					return dlJob{}, err
				}
				chosen, err := asset.PromptFromCandidates(ac)
				if err != nil {
					return dlJob{}, err
				}
				return dlJob{name: name, source: source, version: ver, pinned: pinned, release: rel, chosen: chosen}, nil
			},
		})
	}

	results := parallel.Run(cmd.Context(), tasks, cfg.NumParallel)
	var ready []dlJob
	for _, res := range results {
		if errors.Is(res.Err, asset.ErrSkip) {
			continue
		}
		if res.Err != nil {
			printFail(cfg, "%s: %v", res.Name, res.Err)
			hadErrors = true
			continue
		}
		printInfo(cfg, "%s: asset: %s", res.Name, res.Value.chosen.Name)
		ready = append(ready, res.Value)
	}
	if len(ready) == 0 {
		if hadErrors {
			return errSilent
		}
		return nil
	}

	if dryRun {
		for _, r := range ready {
			fmt.Printf("%s: download %s (asset: %s)\n", r.name, config.NormalizeVersion(r.release.TagName), r.chosen.Name)
		}
		return nil
	}

	sep()
	if !promptConfirm(fmt.Sprintf("download %d asset(s)", len(ready))) {
		return nil
	}

	dlTasks := make([]parallel.Task[struct{}], len(ready))
	for i, r := range ready {
		dlTasks[i] = parallel.Task[struct{}]{
			Name: r.name,
			Run: func() (struct{}, error) {
				owner, repo, _ := gh.SplitSource(r.source)
				dest := destPath
				if dest == "" {
					var err error
					dest, err = dirs.ReleaseDir(r.source, r.release.TagName)
					if err != nil {
						return struct{}{}, err
					}
				}
				return struct{}{}, ghClient.DownloadAsset(ctx, owner, repo, r.release.TagName, r.chosen.Name, dest)
			},
		}
	}

	for _, res := range parallel.Run(cmd.Context(), dlTasks, cfg.NumParallel) {
		if res.Err != nil {
			printFail(cfg, "%s: %v", res.Name, res.Err)
			hadErrors = true
		} else {
			printPass(cfg, "downloaded %s", res.Name)
		}
	}

	if hadErrors {
		return errSilent
	}
	return nil
}
