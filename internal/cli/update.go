package cli

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/asset"
	"github.com/meop/ghpm/internal/config"
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
	cfg, err := config.LoadSettings()
	if err != nil {
		return err
	}
	if cfg.NoVerify {
		noVerify = true
	}
	manifest, err := config.LoadManifest()
	if err != nil {
		return err
	}
	if err := gh.CheckInstalled(); err != nil {
		return err
	}

	// Refresh repos from remote (update is the only command that fetches fresh repos)
	if _, err := config.RefreshRepos(); err != nil {
		color.Yellow("⚠ could not refresh repos: %v", err)
	}

	// Build target list — skip fixed pins
	targets := map[string]config.PackageEntry{}
	if len(args) == 0 {
		for k, p := range manifest.Installs {
			if p.Pin != "fixed" {
				targets[k] = p
			}
		}
	} else {
		for _, name := range args {
			p, ok := manifest.Installs[name]
			if !ok {
				color.Yellow("⚠ %s: not installed", name)
				continue
			}
			if p.Pin == "fixed" {
				color.Yellow("⚠ %s is fixed at %s, skipping", name, p.Version)
				continue
			}
			targets[name] = p
		}
	}
	if len(targets) == 0 {
		return nil
	}

	type updateJob struct {
		key     string
		source  string
		pkg     config.PackageEntry
		release gh.Release
		chosen  gh.Asset
	}

	tasks := make([]parallel.Task, 0, len(targets))
	for key, pkg := range targets {
		name, verStr, isPinned := config.ParseVersionSuffix(key)
		source := manifest.Repos[name]
		var c config.Constraint
		if isPinned {
			c, _ = config.ParseConstraint(verStr)
		}
		tasks = append(tasks, parallel.Task{
			Name: key,
			Run: func() (any, error) {
				owner, repo, err := gh.SplitSource(source)
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
				if config.CompareVersions(latest, pkg.Version) <= 0 {
					return nil, nil // already latest within constraint
				}
				chosen, err := asset.SelectAsset(rel.Assets, cfg, "")
				if err != nil {
					return nil, err
				}
				return updateJob{key: key, source: source, pkg: pkg, release: rel, chosen: chosen}, nil
			},
		})
	}

	results := parallel.Run(cmd.Context(), tasks, cfg.Parallelism)
	var ready []updateJob
	for _, res := range results {
		if res.Err != nil {
			color.Yellow("⚠ %s: %v", res.Name, res.Err)
			continue
		}
		if res.Value == nil {
			fmt.Printf("  %s is already up to date\n", res.Name)
			continue
		}
		uj, ok := res.Value.(updateJob)
		if !ok {
			continue
		}
		ready = append(ready, uj)
	}
	if len(ready) == 0 {
		return nil
	}

	if dryRun {
		for _, r := range ready {
			fmt.Printf("[dry-run] would update %s %s → %s (asset: %s)\n", r.key, r.pkg.Version, r.release.TagName, r.chosen.Name)
		}
		return nil
	}

	if !promptConfirm(fmt.Sprintf("Update %d package(s)?", len(ready))) {
		fmt.Println("Aborted.")
		return nil
	}

	updateTasks := make([]parallel.Task, len(ready))
	for i, r := range ready {
		updateTasks[i] = parallel.Task{
			Name: r.key,
			Run: func() (any, error) {
				owner, repo, _ := gh.SplitSource(r.source)
				cacheDir, err := store.ReleaseDir(r.source, r.release.TagName)
				if err != nil {
					return nil, err
				}
				if err := gh.DownloadAsset(owner, repo, r.release.TagName, r.chosen.Name, cacheDir); err != nil {
					return nil, err
				}
				if !noVerify {
					if err := asset.VerifySHA(owner, repo, r.release.TagName, cacheDir, r.chosen.Name, r.release.Assets); err != nil {
						return nil, fmt.Errorf("SHA verification failed: %w", err)
					}
				}
				binDir, err := store.BinDir()
				if err != nil {
					return nil, err
				}
				if _, err := asset.Extract(cacheDir, r.chosen.Name, binDir, r.key, ""); err != nil {
					return nil, err
				}
				return r, nil
			},
		}
	}

	for _, res := range parallel.Run(cmd.Context(), updateTasks, cfg.Parallelism) {
		if res.Err != nil {
			color.Yellow("⚠ %s: %v", res.Name, res.Err)
			continue
		}
		r, ok := res.Value.(updateJob)
		if !ok {
			continue
		}
		newVer := config.NormalizeVersion(r.release.TagName)
		manifest.Installs[r.key] = config.PackageEntry{
			Pin:     r.pkg.Pin,
			Version: newVer,
		}
		color.Green("✓ updated %s %s → %s", r.key, r.pkg.Version, newVer)
	}

	return config.SaveManifest(manifest)
}
