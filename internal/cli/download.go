package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/asset"
	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/parallel"
	"github.com/meop/ghpm/internal/store"
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
	cmd.Flags().BoolVarP(&noVerify, "skip-verify", "s", false, "Skip SHA256 verification")
	return cmd
}

func runDownload(cmd *cobra.Command, args []string) error {
	destPath, _ := cmd.Flags().GetString("path")
	ci, err := initCommand(cmdOptions{Manifest: true, GH: true, Repos: true, NoVerify: true})
	if err != nil {
		return err
	}
	cfg := ci.cfg
	manifest := ci.manifest
	repos := ci.repos
	ctx := cmd.Context()

	type dlJob struct {
		name    string
		source  string
		version string
		pinned  bool
		release gh.Release
		chosen  gh.Asset
	}

	tasks := make([]parallel.Task, 0, len(args))
	var hadErrors bool
	for _, arg := range args {
		name, ver, pinned := config.ParseVersionSuffix(arg)
		fmt.Printf("download: %s\n", name)
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
		tasks = append(tasks, parallel.Task{
			Name: name,
			Run: func() (any, error) {
				owner, repo, err := gh.SplitSource(source)
				if err != nil {
					return nil, err
				}
				var rel gh.Release
				if ver != "" {
					rel, err = gh.GetReleaseByTag(ctx, owner, repo, ver)
				} else {
					rel, err = gh.GetLatestRelease(ctx, owner, repo)
				}
				if err != nil {
					return nil, err
				}
				ac, err := asset.SelectAssetAuto(rel.Assets, cfg, "", name)
				if err != nil {
					return nil, err
				}
				chosen, err := asset.PromptFromCandidates(ac)
				if err != nil {
					return nil, err
				}
				if ac.Chosen.Name != "" {
					printInfo(cfg, "asset: %s", chosen.Name)
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
		r, ok := res.Value.(dlJob)
		if !ok {
			continue
		}
		ready = append(ready, r)
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

	if !promptConfirm(fmt.Sprintf("download %d asset(s)", len(ready))) {
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
				return nil, gh.DownloadAsset(ctx, owner, repo, r.release.TagName, r.chosen.Name, dest)
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
