package cli

import (
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
		Use:   "download <name> [name...]",
		Short: "Download release assets without extracting",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runDownload,
	}
	cmd.Flags().String("path", "", "Destination directory (default: ~/.ghpm/releases/)")
	return cmd
}

func runDownload(cmd *cobra.Command, args []string) error {
	destPath, _ := cmd.Flags().GetString("path")
	cfg, err := config.LoadSettings()
	if err != nil {
		printFail(nil, "could not load settings: %v", err)
		return errSilent
	}
	if cfg.NoVerify {
		noVerify = true
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
	repos, repoErr := config.LoadRepos()
	if repoErr != nil {
		printInfo(cfg, "could not load repos: %v", repoErr)
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
	var hadErrors bool
	for _, arg := range args {
		name, ver, pinned := config.ParseVersionSuffix(arg)
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
					rel, err = gh.GetReleaseByTag(owner, repo, ver)
				} else {
					rel, err = gh.GetLatestRelease(owner, repo)
				}
				if err != nil {
					return nil, err
				}
				chosen, err := asset.SelectAsset(rel.Assets, cfg, "")
				if err != nil {
					return nil, err
				}
				return dlJob{name: name, source: source, version: ver, pinned: pinned, release: rel, chosen: chosen}, nil
			},
		})
	}

	results := parallel.Run(cmd.Context(), tasks, cfg.NumParallel)
	var ready []dlJob
	for _, res := range results {
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
			fmt.Printf("[dry-run] would download %s %s (asset: %s)\n", r.name, config.NormalizeVersion(r.release.TagName), r.chosen.Name)
		}
		return nil
	}

	if !promptConfirm(fmt.Sprintf("download %d asset(s)", len(ready))) {
		fmt.Println("aborted")
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
