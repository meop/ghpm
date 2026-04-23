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

func newInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install <name> [name...]",
		Short: "Install packages from GitHub Releases",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runInstall,
	}
}

type installJob struct {
	name    string
	source  string
	version string
	pinned  bool
}

type jobWithRelease struct {
	job        installJob
	release    gh.Release
	chosen     gh.Asset
	binaryName string // set after extraction
}

func runInstall(cmd *cobra.Command, args []string) error {
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
	if err := config.EnsureDirs(); err != nil {
		return err
	}
	if err := gh.CheckInstalled(); err != nil {
		return err
	}

	aliases, aliasErr := config.FetchAliases()
	if aliasErr != nil {
		color.Yellow("⚠ could not fetch aliases (offline?): %v", aliasErr)
	}

	jobs := make([]installJob, 0, len(args))
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
		// Deduplication: warn if this source is already installed under another name
		if existing, found := config.FindBySource(source, manifest); found {
			if existing == name {
				color.Yellow("⚠ %s is already installed — use 'ghpm update %s' to upgrade", name, name)
			} else {
				color.Yellow("⚠ %s (%s) is already installed as %q — skipping", name, source, existing)
			}
			continue
		}
		jobs = append(jobs, installJob{name: name, source: source, version: ver, pinned: pinned})
	}
	if len(jobs) == 0 {
		return nil
	}

	// Resolve releases and pick assets in parallel
	tasks := make([]parallel.Task, len(jobs))
	for i, job := range jobs {
		job := job
		tasks[i] = parallel.Task{
			Name: job.name,
			Run: func() (any, error) {
				owner, repo, err := gh.SplitSource(job.source)
				if err != nil {
					return nil, err
				}
				var rel gh.Release
				if !job.pinned {
					rel, err = gh.GetLatestRelease(owner, repo)
				} else {
					c, perr := config.ParseConstraint(job.version)
					if perr != nil {
						return nil, perr
					}
					if c.Level == config.PinExact {
						rel, err = gh.GetReleaseByTag(owner, repo, job.version)
					} else {
						rel, err = gh.FindLatestMatching(owner, repo, c)
					}
				}
				if err != nil {
					return nil, err
				}

				// Use existing asset_pattern as a hint if reinstalling
				manifestKey := job.name
				if job.pinned {
					manifestKey = job.name + "@" + job.version
				}
				var assetHint string
				if existing, ok := manifest.Packages[manifestKey]; ok {
					assetHint = existing.AssetPattern
				}

				chosen, err := asset.SelectAsset(rel.Assets, cfg, assetHint)
				if err != nil {
					return nil, err
				}
				return jobWithRelease{job: job, release: rel, chosen: chosen}, nil
			},
		}
	}

	results := parallel.Run(cmd.Context(), tasks, cfg.Parallelism)

	var ready []jobWithRelease
	for _, res := range results {
		if res.Err != nil {
			color.Red("✗ %s: %v", res.Name, res.Err)
			continue
		}
		ready = append(ready, res.Value.(jobWithRelease))
	}
	if len(ready) == 0 {
		return nil
	}

	if DryRun {
		for _, r := range ready {
			fmt.Printf("[dry-run] would install %s %s (asset: %s)\n", r.job.name, r.release.TagName, r.chosen.Name)
		}
		return nil
	}

	if !promptInstall(ready) {
		fmt.Println("Aborted.")
		return nil
	}

	// Download + verify + extract in parallel
	installTasks := make([]parallel.Task, len(ready))
	for i, r := range ready {
		r := r
		installTasks[i] = parallel.Task{
			Name: r.job.name,
			Run: func() (any, error) {
				owner, repo, _ := gh.SplitSource(r.job.source)
				cacheDir, err := store.ReleaseDir(r.job.source, r.release.TagName)
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
				outputName := r.job.name
				if r.job.pinned {
					outputName = versionedBinName(r.job.name, r.job.version)
				}
				binaryName, err := asset.Extract(cacheDir, r.chosen.Name, binDir, outputName, "")
				if err != nil {
					return nil, err
				}
				r.binaryName = binaryName
				return r, nil
			},
		}
	}

	installResults := parallel.Run(cmd.Context(), installTasks, cfg.Parallelism)

	for _, res := range installResults {
		if res.Err != nil {
			color.Red("✗ %s: %v", res.Name, res.Err)
			continue
		}
		r := res.Value.(jobWithRelease)
		key := r.job.name
		if r.job.pinned {
			key = r.job.name + "@" + r.job.version
		}
		manifest.Packages[key] = config.PackageEntry{
			Source:       r.job.source,
			Version:      config.NormalizeVersion(r.release.TagName),
			Versioned:    r.job.pinned,
			AssetPattern: r.chosen.Name,
			BinaryName:   r.binaryName,
			InstalledAt:  config.Now(),
		}
		color.Green("✓ installed %s %s", r.job.name, config.NormalizeVersion(r.release.TagName))
	}

	return config.SaveManifest(manifest)
}

func promptInstall(ready []jobWithRelease) bool {
	if len(ready) == 1 {
		r := ready[0]
		fmt.Printf("Install %s %s? [y/N] ", r.job.name, r.release.TagName)
	} else {
		fmt.Print("Install ")
		for i, r := range ready {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Printf("%s %s", r.job.name, r.release.TagName)
		}
		fmt.Print("? [y/N] ")
	}
	return readYN()
}
