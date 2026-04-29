package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/asset"
	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/env"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/parallel"
	"github.com/meop/ghpm/internal/store"
)

func newUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update [name...]",
		Short: "Update packages to their latest releases",
		RunE:  runUpdate,
	}
}

func runUpdate(cmd *cobra.Command, args []string) error {
	unlock, err := config.AcquireLock()
	if err != nil {
		printFail(nil, "%v", err)
		return errSilent
	}
	defer unlock()

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

	syncResults, _ := config.RefreshRepos()
	var hadErrors bool
	for _, r := range syncResults {
		if r.Err != nil {
			printFail(cfg, "%s %v", r.Source, r.Err)
			hadErrors = true
		} else {
			printPass(cfg, "synced %s (%d entries)", r.Source, r.Count)
		}
	}

	targets := map[string]config.PackageEntry{}
	if len(args) == 0 {
		for k, p := range manifest.Extracts {
			if p.Pin != "fixed" {
				targets[k] = p
			}
		}
	} else {
		for _, name := range args {
			p, ok := manifest.Extracts[name]
			if !ok {
				printInfo(cfg, "%s not installed", name)
				continue
			}
			if p.Pin == "fixed" {
				printInfo(cfg, "%s is fixed at %s, skipping", name, p.Version)
				continue
			}
			targets[name] = p
		}
	}
	if len(targets) == 0 {
		if hadErrors {
			return errSilent
		}
		return nil
	}

	items := make([]gh.BatchItem, 0, len(targets))
	for key := range targets {
		name, verStr, isPinned := config.ParseVersionSuffix(key)
		source := manifest.Repos[name]
		var c config.Constraint
		if isPinned {
			c, _ = config.ParseConstraint(verStr)
		}
		items = append(items, gh.BatchItem{
			Key:    key,
			Source: source,
			Pin:    c,
		})
	}

	batchResults := gh.BatchLatestVersions(items, cfg.CacheTTL)

	type updateJob struct {
		key     string
		source  string
		pkg     config.PackageEntry
		release gh.Release
		chosen  gh.Asset
	}

	var ready []updateJob
	checked := 0
	skipped := 0

	for _, res := range batchResults {
		if res.Err != nil {
			if gh.IsRateLimited(res.Err) {
				skipped++
				printWarn(cfg, "%s rate limited", res.Key)
				continue
			}
			printFail(cfg, "%s %v", res.Key, res.Err)
			hadErrors = true
			continue
		}
		checked++
		pkg := targets[res.Key]
		latest := config.NormalizeVersion(res.LatestTag)
		if config.CompareVersions(latest, pkg.Version) <= 0 {
			continue
		}

		owner, repo, _ := gh.SplitSource(items[0].Source)
		for _, it := range items {
			if it.Key == res.Key {
				owner, repo, _ = gh.SplitSource(it.Source)
				break
			}
		}
		rel, err := gh.GetReleaseByTag(owner, repo, res.LatestTag)
		if err != nil {
			printFail(cfg, "%s %v", res.Key, err)
			hadErrors = true
			continue
		}
		chosen, err := asset.SelectAsset(rel.Assets, cfg, pkg.AssetName)
		if err != nil {
			printFail(cfg, "%s %v", res.Key, err)
			hadErrors = true
			continue
		}
		job := updateJob{key: res.Key, source: items[0].Source, pkg: pkg, release: rel, chosen: chosen}
		for _, it := range items {
			if it.Key == res.Key {
				job.source = it.Source
				break
			}
		}
		ready = append(ready, job)
	}

	if skipped > 0 {
		fmt.Printf("\nchecked %d/%d packages (%d skipped due to rate limiting)\n", checked, len(items), skipped)
	}

	if len(ready) == 0 {
		if skipped == 0 {
			printInfo(cfg, "all packages are up to date")
		}
		if hadErrors {
			return errSilent
		}
		return nil
	}

	if dryRun {
		rows := make([][]string, len(ready))
		for i, r := range ready {
			rows[i] = []string{r.key, r.pkg.Pin, r.pkg.Version, config.NormalizeVersion(r.release.TagName), r.chosen.Name, r.source}
		}
		colors := []func(string) string{nil, nil, colorfn(cfg, "old"), colorfn(cfg, "new"), nil, nil}
		printTable([]string{"name", "pin", "version", "update", "asset", "repo"}, rows, colors)
		return nil
	}

	if !promptConfirm(fmt.Sprintf("update %d package(s)", len(ready))) {
		fmt.Println("aborted")
		return nil
	}

	updateTasks := make([]parallel.Task, len(ready))
	for i, r := range ready {
		updateTasks[i] = parallel.Task{
			Name: r.key,
			Run: func() (any, error) {
				fmt.Printf("  downloading %s %s...\n", r.key, config.NormalizeVersion(r.release.TagName))
				owner, repo, _ := gh.SplitSource(r.source)
				cacheDir, err := store.ReleaseDir(r.source, r.release.TagName)
				if err != nil {
					return nil, err
				}
				if err := gh.DownloadAsset(owner, repo, r.release.TagName, r.chosen.Name, cacheDir); err != nil {
					return nil, err
				}
				if !noVerify {
					_, err := asset.Verify(owner, repo, r.release.TagName, cacheDir, r.chosen.Name)
					if err != nil {
						return nil, err
					}
				}
				pkgDir, err := store.ExtractDir(r.key)
				if err != nil {
					return nil, err
				}
				if err := os.RemoveAll(pkgDir); err != nil {
					return nil, err
				}
				if err := asset.ExtractPackage(cacheDir, r.chosen.Name, pkgDir); err != nil {
					return nil, err
				}
				return r, nil
			},
		}
	}

	for _, res := range parallel.Run(cmd.Context(), updateTasks, cfg.NumParallel) {
		if res.Err != nil {
			printFail(cfg, "%s %v", res.Name, res.Err)
			hadErrors = true
			continue
		}
		r, ok := res.Value.(updateJob)
		if !ok {
			continue
		}
		pkgDir, _ := store.ExtractDir(r.key)
		binPath, binaryName := asset.DiscoverPaths(pkgDir)
		newVer := config.NormalizeVersion(r.release.TagName)
		manifest.Extracts[r.key] = config.PackageEntry{
			Pin:        r.pkg.Pin,
			Version:    newVer,
			AssetName:      r.chosen.Name,
			BinDir:    binPath,
			BinName: binaryName,
		}
		printPass(cfg, "updated %s %s → %s", r.key, r.pkg.Version, newVer)
	}

	if err := config.SaveManifest(manifest); err != nil {
		printFail(cfg, "could not save manifest: %v", err)
		return errSilent
	}

	if _, err := env.Generate(manifest); err != nil {
		printWarn(cfg, "could not generate env files: %v", err)
	}

	if hadErrors {
		return errSilent
	}
	return nil
}
