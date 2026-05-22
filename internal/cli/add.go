package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/asset"
	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/parallel"
	"github.com/meop/ghpm/internal/shim"
	"github.com/meop/ghpm/internal/store"
)

var forceInstall bool
var installCfg *config.Settings

func newAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "add <name> [name...]",
		Aliases: []string{"ad", "in", "install"},
		Short:   "Add packages from releases",
		Args:    cobra.MinimumNArgs(1),
		RunE:    runAdd,
	}
	cmd.Flags().BoolVarP(&forceInstall, "force", "f", false, "Reinstall even if already installed")
	cmd.Flags().BoolVarP(&noVerify, "skip-verify", "s", false, "Skip SHA256 verification")
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

func runAdd(cmd *cobra.Command, args []string) error {
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

	var ready []jobWithRelease
	var hadErrors bool

	for _, arg := range args {
		name, ver, pinned := config.ParseVersionSuffix(arg)
		if name == binGhpm || name == binGh {
			printInfo(cfg, "%s: managed by ghpm upgrade, skipping", name)
			continue
		}
		if err := config.ValidateName(name); err != nil {
			printFail(cfg, "%v", err)
			hadErrors = true
			continue
		}

		source, found := config.LookupSource(name, manifest, repos)
		if !found {
			printInfo(cfg, "repo not defined")
			var err error
			source, err = config.SearchGitHub(name)
			if err != nil {
				printFail(cfg, "%v", err)
				hadErrors = true
				continue
			}
		}
		printInfo(cfg, "repo defined: %s", source)

		jobKey := name
		if pinned {
			jobKey = name + "@" + strings.TrimPrefix(ver, "v")
		}
		if entry, exists := manifest.Extracts[jobKey]; exists && !forceInstall {
			printInfo(cfg, "%s: already installed %s", jobKey, entry.Version)
			continue
		}
		if !pinned {
			if existing, found := config.FindBySource(source, manifest); found && existing != name && !forceInstall {
				printInfo(cfg, "%s: already installed as %s — skipping", name, existing)
				continue
			}
		}

		owner, repo, err := gh.SplitSource(source)
		if err != nil {
			printFail(cfg, "%v", err)
			hadErrors = true
			continue
		}
		var rel gh.Release
		if !pinned {
			rel, err = gh.GetLatestRelease(owner, repo)
		} else {
			c, perr := config.ParseConstraint(ver)
			if perr != nil {
				printFail(cfg, "%v", perr)
				hadErrors = true
				continue
			}
			if c.Level == config.PinExact {
				rel, err = gh.GetReleaseByTag(owner, repo, ver)
			} else {
				rel, err = gh.FindLatestMatching(owner, repo, c)
			}
		}
		if err != nil {
			printFail(cfg, "%v", err)
			hadErrors = true
			continue
		}

		ac, err := asset.SelectAssetAuto(rel.Assets, cfg, "", name)
		if err != nil {
			printFail(cfg, "%v", err)
			hadErrors = true
			continue
		}
		chosen, err := asset.PromptFromCandidates(ac)
		if errors.Is(err, asset.ErrSkip) {
			continue
		}
		if err != nil {
			printFail(cfg, "%v", err)
			hadErrors = true
			continue
		}
		if ac.Chosen.Name != "" {
			printInfo(cfg, "asset: %s", chosen.Name)
		}
		ready = append(ready, jobWithRelease{
			job:     installJob{name: name, source: source, version: ver, pinned: pinned},
			release: rel,
			chosen:  chosen,
		})
	}

	if len(ready) == 0 {
		if hadErrors {
			return errSilent
		}
		return nil
	}

	if dryRun {
		for _, r := range ready {
			fmt.Printf("[dry-run] %s: would install %s (asset: %s)\n", r.job.name, config.NormalizeVersion(r.release.TagName), r.chosen.Name)
		}
		return nil
	}

	if !promptInstall(ready) {
		return nil
	}

	installTasks := make([]parallel.Task, len(ready))
	for i, r := range ready {
		installTasks[i] = parallel.Task{
			Name: r.job.name,
			Run: func() (any, error) {
				printInfo(cfg, "%s: downloading %s...", r.job.name, config.NormalizeVersion(r.release.TagName))
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
				version := config.NormalizeVersion(r.release.TagName)
				pkgDir, err := store.ExtractDir(r.job.key(), version)
				if err != nil {
					return nil, err
				}
				if err := os.RemoveAll(pkgDir); err != nil {
					return nil, err
				}
				if err := asset.ExtractPackage(cacheDir, r.chosen.Name, pkgDir); err != nil {
					_ = os.RemoveAll(pkgDir)
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
		pkgDir, _ := store.ExtractDir(r.job.key(), config.NormalizeVersion(r.release.TagName))
		candidates := asset.FindBinaries(pkgDir, r.job.name)
		selected, discoverErr := asset.SelectBinaries(candidates, r.job.name, nil)
		if errors.Is(discoverErr, asset.ErrSkip) {
			continue
		}
		if len(selected) == 0 {
			printFail(cfg, "%s: no binary found in %s", r.job.name, r.chosen.Name)
			hadErrors = true
			continue
		}
		binNames := make([]string, len(selected))
		for i, s := range selected {
			binNames[i] = s.BinName
		}
		printInfo(cfg, "%s: binary %s", r.job.name, strings.Join(binNames, ", "))
		key := r.job.key()
		manifest.Repos[r.job.name] = r.job.source
		manifest.Extracts[key] = config.PackageEntry{
			Pin:       r.job.pin(),
			Version:   config.NormalizeVersion(r.release.TagName),
			AssetName: r.chosen.Name,
			BinDir:    selected[0].BinDir,
			BinNames:  binNames,
		}
		for _, s := range selected {
			if err := shim.Create(binShimName(key, s.BinName), s.BinName, pkgDir, s.BinDir); err != nil {
				printWarn(cfg, "%s: could not create shim: %v", s.BinName, err)
			}
		}
		printPass(cfg, "%s: installed %s", r.job.name, config.NormalizeVersion(r.release.TagName))
	}

	if err := config.SaveManifest(manifest); err != nil {
		printFail(cfg, "could not save manifest: %v", err)
		return errSilent
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
