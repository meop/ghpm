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

	// resolved is the parallel phase's output: the network fetch plus scored
	// asset candidates. Picking among ambiguous candidates may prompt, so it is
	// deferred to a sequential phase — interactive prompts must never run inside
	// parallel workers (interleaved menus, racy stdin, shared ui state).
	type resolved struct {
		job dlJob
		ac  asset.AssetCandidates
	}

	tasks := make([]parallel.Task[resolved], 0, len(args))
	var hadErrors bool
	var failedItems []failedItem
	for _, arg := range args {
		name, ver, pinned := config.ParseVersionSuffix(arg)
		if err := config.ValidateName(name); err != nil {
			printFail(cfg, "%s: %v", name, err)
			hadErrors = true
			failedItems = append(failedItems, failedItem{name: name, reason: err.Error()})
			continue
		}
		source, err := config.ResolveSource(name, ver, manifest, repos)
		if err != nil {
			printFail(cfg, "%s: %v", name, err)
			hadErrors = true
			failedItems = append(failedItems, failedItem{name: name, reason: err.Error()})
			continue
		}
		tasks = append(tasks, parallel.Task[resolved]{
			Name: name,
			Run: func() (resolved, error) {
				owner, repo, err := gh.SplitSource(source)
				if err != nil {
					return resolved{}, err
				}
				var rel gh.Release
				if ver != "" {
					rel, err = ghClient.GetReleaseByTag(ctx, owner, repo, ver)
				} else {
					rel, err = ghClient.GetLatestRelease(ctx, owner, repo)
				}
				if err != nil {
					return resolved{}, err
				}
				ac, err := asset.SelectAssetAuto(rel.Assets, cfg, "", name)
				if err != nil {
					return resolved{}, err
				}
				return resolved{
					job: dlJob{name: name, source: source, version: ver, pinned: pinned, release: rel},
					ac:  ac,
				}, nil
			},
		})
	}

	var pending []resolved
	for _, res := range parallel.Run(cmd.Context(), tasks, cfg.NumParallel) {
		if res.Err != nil {
			printFail(cfg, "%s: %v", res.Name, res.Err)
			hadErrors = true
			failedItems = append(failedItems, failedItem{name: res.Name, reason: res.Err.Error()})
			continue
		}
		pending = append(pending, res.Value)
	}
	if len(pending) == 0 {
		if hadErrors {
			printFailedTable(cfg, "asset", failedItems)
			return errSilent
		}
		return nil
	}

	// Gate: show what will be downloaded and let the user bail before any asset
	// prompt or download. Assets aren't chosen until after opt-in, so no asset
	// column.
	introRows := make([][]string, 0, len(pending))
	for _, p := range pending {
		introRows = append(introRows, []string{p.job.name, config.NormalizeVersion(p.job.release.TagName), p.job.source})
	}
	if !gate([]string{"name", "version", "repo"}, introRows, []func(string) string{nil, colorfn(cfg, "new"), nil}, fmt.Sprintf("download %d asset(s)", len(pending))) {
		return nil
	}

	// After opt-in, resolve each asset, prompting only where ambiguous. A skipped
	// package drops out.
	var ready []dlJob
	for _, p := range pending {
		chosen, err := asset.PromptFromCandidates(p.ac, p.job.name)
		if errors.Is(err, asset.ErrSkip) {
			continue
		}
		if err != nil {
			printFail(cfg, "%s: %v", p.job.name, err)
			hadErrors = true
			failedItems = append(failedItems, failedItem{name: p.job.name, reason: err.Error()})
			continue
		}
		job := p.job
		job.chosen = chosen
		ready = append(ready, job)
	}
	if len(ready) == 0 {
		if hadErrors {
			printFailedTable(cfg, "asset", failedItems)
			return errSilent
		}
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
				return struct{}{}, downloadAsset(ctx, ghClient, owner, repo, r.release.TagName, r.chosen.Name, dest, r.name)
			},
		}
	}

	successCount := 0
	for _, res := range parallel.Run(cmd.Context(), dlTasks, cfg.NumParallel) {
		if res.Err != nil {
			printFail(cfg, "%s: %v", res.Name, res.Err)
			hadErrors = true
			failedItems = append(failedItems, failedItem{name: res.Name, reason: res.Err.Error()})
		} else {
			successCount++
		}
	}
	sep()
	if successCount > 0 {
		printPass(cfg, "downloaded %d asset(s)", successCount)
	}
	printFailedTable(cfg, "asset", failedItems)

	if hadErrors {
		return errSilent
	}
	return nil
}
