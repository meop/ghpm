package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/asset"
	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/env"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/parallel"
	"github.com/meop/ghpm/internal/store"
)

var forceInstall bool
var installCfg *config.Settings

func newInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install <name> [name...]",
		Short: "Install packages from GitHub Releases",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runInstall,
	}
	cmd.Flags().BoolVarP(&forceInstall, "force", "f", false, "Reinstall even if already installed")
	return cmd
}

type installJob struct {
	name    string
	source  string
	version string
	pinned  bool
}

func (j installJob) pin() string {
	if !j.pinned {
		return "latest"
	}
	c, err := config.ParseConstraint(j.version)
	if err != nil {
		return "latest"
	}
	return c.Level.String()
}

func (j installJob) key() string {
	if !j.pinned {
		return j.name
	}
	return j.name + "@" + strings.TrimPrefix(j.version, "v")
}

type jobWithRelease struct {
	job     installJob
	release gh.Release
	chosen  gh.Asset
}

func runInstall(cmd *cobra.Command, args []string) error {
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
	installCfg = cfg
	if cfg.NoVerify {
		noVerify = true
	}
	manifest, err := config.LoadManifest()
	if err != nil {
		printFail(cfg, "could not load manifest: %v", err)
		return errSilent
	}
	if err := config.EnsureDirs(); err != nil {
		printFail(cfg, "%v", err)
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

	jobs := make([]installJob, 0, len(args))
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
		jobKey := name
		if pinned {
			jobKey = name + "@" + strings.TrimPrefix(ver, "v")
		}
		if entry, exists := manifest.Extracts[jobKey]; exists && !forceInstall {
			printInfo(cfg, "%s %s is already installed", jobKey, entry.Version)
			continue
		}
		if !pinned {
			if existing, found := config.FindBySource(source, manifest); found && existing != name && !forceInstall {
				printInfo(cfg, "%s (%s) is already installed as %q — skipping", name, source, existing)
				continue
			}
		}
		jobs = append(jobs, installJob{name: name, source: source, version: ver, pinned: pinned})
	}
	if len(jobs) == 0 {
		if hadErrors {
			return errSilent
		}
		return nil
	}

	type jobWithAssets struct {
		job       installJob
		release   gh.Release
		candidates asset.AssetCandidates
	}

	fetchTasks := make([]parallel.Task, len(jobs))
	for i, job := range jobs {
		fetchTasks[i] = parallel.Task{
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

				ac, err := asset.SelectAssetAuto(rel.Assets, cfg, "")
				if err != nil {
					return nil, err
				}
				return jobWithAssets{job: job, release: rel, candidates: ac}, nil
			},
		}
	}

	fetchResults := parallel.Run(cmd.Context(), fetchTasks, cfg.NumParallel)

	var ready []jobWithRelease
	for _, res := range fetchResults {
		if res.Err != nil {
			printFail(cfg, "%s %v", res.Name, res.Err)
			hadErrors = true
			continue
		}
		ja, ok := res.Value.(jobWithAssets)
		if !ok {
			continue
		}
		var chosen gh.Asset
		if ja.candidates.Chosen.Name != "" {
			chosen = ja.candidates.Chosen
		} else if len(ja.candidates.Ambiguous) > 0 {
			var err error
			chosen, err = asset.PromptSelect("Multiple candidates found. Select one:", ja.candidates.Ambiguous)
			if err != nil {
				printFail(cfg, "%s %v", ja.job.name, err)
				hadErrors = true
				continue
			}
		} else {
			var err error
			chosen, err = asset.PromptSelect("No auto-matched assets. Select one:", ja.candidates.All)
			if err != nil {
				printFail(cfg, "%s %v", ja.job.name, err)
				hadErrors = true
				continue
			}
		}
		ready = append(ready, jobWithRelease{job: ja.job, release: ja.release, chosen: chosen})
	}
	if len(ready) == 0 {
		if hadErrors {
			return errSilent
		}
		return nil
	}

	if dryRun {
		for _, r := range ready {
			fmt.Printf("[dry-run] would install %s %s (asset: %s)\n", r.job.name, config.NormalizeVersion(r.release.TagName), r.chosen.Name)
		}
		return nil
	}

	if !promptInstall(ready) {
		fmt.Println("aborted")
		return nil
	}

	installTasks := make([]parallel.Task, len(ready))
	for i, r := range ready {
		installTasks[i] = parallel.Task{
			Name: r.job.name,
			Run: func() (any, error) {
				fmt.Printf("downloading %s %s...\n", r.job.name, config.NormalizeVersion(r.release.TagName))
				owner, repo, _ := gh.SplitSource(r.job.source)
				cacheDir, err := store.ReleaseDir(r.job.source, r.release.TagName)
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
				pkgDir, err := store.ExtractDir(r.job.key())
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

	installResults := parallel.Run(cmd.Context(), installTasks, cfg.NumParallel)

	for _, res := range installResults {
		if res.Err != nil {
			printFail(cfg, "%s: %v", res.Name, res.Err)
			hadErrors = true
			continue
		}
		r, ok := res.Value.(jobWithRelease)
		if !ok {
			continue
		}
		pkgDir, _ := store.ExtractDir(r.job.key())
		binPath, binaryName := asset.DiscoverPaths(pkgDir)
		key := r.job.key()
		manifest.Repos[r.job.name] = r.job.source
		manifest.Extracts[key] = config.PackageEntry{
			Pin:        r.job.pin(),
			Version:    config.NormalizeVersion(r.release.TagName),
			AssetName:      r.chosen.Name,
			BinDir:    binPath,
			BinName: binaryName,
		}
		printPass(cfg, "installed %s %s", r.job.name, config.NormalizeVersion(r.release.TagName))
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

func promptInstall(ready []jobWithRelease) bool {
	rows := make([][]string, len(ready))
	for i, r := range ready {
		rows[i] = []string{r.job.key(), r.job.pin(), config.NormalizeVersion(r.release.TagName), r.chosen.Name, r.job.source}
	}
	colors := []func(string) string{nil, nil, colorfn(installCfg, "new"), nil, nil}
	fmt.Println()
	printTable([]string{"name", "pin", "update", "asset", "repo"}, rows, colors)
	fmt.Println()
	return promptConfirm(fmt.Sprintf("install %d package(s)", len(ready)))
}
