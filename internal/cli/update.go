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
		NoVerify = true
	}
	manifest, err := config.LoadManifest()
	if err != nil {
		return err
	}
	if err := gh.CheckInstalled(); err != nil {
		return err
	}

	// Refresh aliases from remote (update is the only command that fetches fresh aliases)
	if _, err := config.RefreshAliases(); err != nil {
		color.Yellow("⚠ could not refresh aliases: %v", err)
	}

	// Build target list — skip exact pins, include floating and major/minor pins
	isExactPin := func(key string) bool {
		_, verStr, isPinned := config.ParseVersionSuffix(key)
		if !isPinned {
			return false
		}
		c, cerr := config.ParseConstraint(verStr)
		if cerr != nil {
			return false
		}
		return c.Level == config.PinExact
	}

	targets := map[string]config.PackageEntry{}
	if len(args) == 0 {
		for k, p := range manifest.Packages {
			if !isExactPin(k) {
				targets[k] = p
			}
		}
	} else {
		for _, name := range args {
			p, ok := manifest.Packages[name]
			if !ok {
				color.Red("✗ %s: not installed", name)
				continue
			}
			if isExactPin(name) {
				color.Yellow("⚠ %s is exactly pinned to %s, skipping", name, p.Version)
				continue
			}
			targets[name] = p
		}
	}
	if len(targets) == 0 {
		return nil
	}

	type updateJob struct {
		key        string
		pkg        config.PackageEntry
		release    gh.Release
		chosen     gh.Asset
		binaryName string
	}

	tasks := make([]parallel.Task, 0, len(targets))
	for key, pkg := range targets {
		_, verStr, isPinned := config.ParseVersionSuffix(key)
		var c config.Constraint
		if isPinned {
			c, _ = config.ParseConstraint(verStr)
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
				if config.CompareVersions(latest, pkg.Version) <= 0 {
					return nil, nil // already latest within constraint
				}
				chosen, err := asset.SelectAsset(rel.Assets, cfg, pkg.AssetPattern)
				if err != nil {
					return nil, err
				}
				return updateJob{key: key, pkg: pkg, release: rel, chosen: chosen}, nil
			},
		})
	}

	results := parallel.Run(cmd.Context(), tasks, cfg.Parallelism)
	var ready []updateJob
	for _, res := range results {
		if res.Err != nil {
			color.Red("✗ %s: %v", res.Name, res.Err)
			continue
		}
		if res.Value == nil {
			fmt.Printf("  %s is already up to date\n", res.Name)
			continue
		}
		ready = append(ready, res.Value.(updateJob))
	}
	if len(ready) == 0 {
		return nil
	}

	if DryRun {
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
				owner, repo, _ := gh.SplitSource(r.pkg.Source)
				cacheDir, err := store.ReleaseDir(r.pkg.Source, r.release.TagName)
				if err != nil {
					return nil, err
				}
				if err := gh.DownloadAsset(owner, repo, r.release.TagName, r.chosen.Name, cacheDir); err != nil {
					return nil, err
				}
				if !NoVerify {
					if err := asset.VerifySHA(owner, repo, r.release.TagName, cacheDir, r.chosen.Name, r.release.Assets); err != nil {
						return nil, fmt.Errorf("SHA verification failed: %w", err)
					}
				}
				binDir, err := store.BinDir()
				if err != nil {
					return nil, err
				}
				binaryName, err := asset.Extract(cacheDir, r.chosen.Name, binDir, r.key, r.pkg.BinaryName)
				if err != nil {
					return nil, err
				}
				r.binaryName = binaryName
				return r, nil
			},
		}
	}

	for _, res := range parallel.Run(cmd.Context(), updateTasks, cfg.Parallelism) {
		if res.Err != nil {
			color.Red("✗ %s: %v", res.Name, res.Err)
			continue
		}
		r := res.Value.(updateJob)
		newVer := config.NormalizeVersion(r.release.TagName)
		manifest.Packages[r.key] = config.PackageEntry{
			Source:       r.pkg.Source,
			Version:      newVer,
			Versioned:    r.pkg.Versioned,
			AssetPattern: r.chosen.Name,
			BinaryName:   r.binaryName,
			InstalledAt:  config.Now(),
		}
		color.Green("✓ updated %s %s → %s", r.key, r.pkg.Version, newVer)
	}

	return config.SaveManifest(manifest)
}
