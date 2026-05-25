package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

		// Detect org/repo or github.com/org/repo form — source is explicit, bypass all lookups.
		var explicitSource string
		if src, repoName, err := parseSourceArg(name); err != nil {
			printFail(cfg, "%v", err)
			hadErrors = true
			continue
		} else if src != "" {
			explicitSource = src
			name = repoName
		}

		if name == binGhpm || name == binGh {
			printInfo(cfg, "%s: self managed, skipping", name)
			continue
		}
		if explicitSource == "" {
			if err := config.ValidateName(name); err != nil {
				printFail(cfg, "%v", err)
				hadErrors = true
				continue
			}
		}

		var source string
		if explicitSource != "" {
			source = explicitSource
			printInfo(cfg, "repo: %s", source)
		} else {
			var found bool
			source, found = config.LookupSource(name, manifest, repos)
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
		}

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
				owner, repo, _ := gh.SplitSource(r.job.source)
				cacheDir, err := store.ReleaseDir(r.job.source, r.release.TagName)
				if err != nil {
					return nil, err
				}
				if _, err := os.Stat(filepath.Join(cacheDir, r.chosen.Name)); os.IsNotExist(err) {
					printInfo(cfg, "%s: downloading %s...", r.job.name, config.NormalizeVersion(r.release.TagName))
					if err := gh.DownloadAsset(owner, repo, r.release.TagName, r.chosen.Name, cacheDir); err != nil {
						return nil, err
					}
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

	type shimPlan struct {
		key       string
		jobName   string
		source    string
		pkgDir    string
		bins      map[string]string
		pin       string
		version   string
		assetName string
		binDir    string
	}
	var shimPlans []shimPlan

	for i, res := range installResults {
		if i > 0 {
			fmt.Println()
		}
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
		rawKeys := make([]string, len(selected))
		for i, s := range selected {
			rawKeys[i] = s.Key()
		}
		printInfo(cfg, "%s: binary %s", r.job.name, strings.Join(rawKeys, ", "))
		key := r.job.key()
		_, _, pinned := config.ParseVersionSuffix(key)
		proposed := proposedShimNames(key, selected)
		reserved := make(map[string]string)
		for mKey, entry := range manifest.Extracts {
			ownerPkg, _, _ := config.ParseVersionSuffix(mKey)
			if ownerPkg == r.job.name {
				continue
			}
			for shimName := range entry.Bins {
				reserved[shimName] = ownerPkg
			}
		}
		for _, prev := range shimPlans {
			if prev.jobName == r.job.name {
				continue
			}
			for shimName := range prev.bins {
				reserved[shimName] = prev.jobName
			}
		}
		shimNames := proposed
		if hasReservedConflict(proposed, reserved) || (!pinned && needsShimRenamePrompt(r.job.name, selected)) {
			var promptErr error
			shimNames, promptErr = asset.PromptShimRenames(r.job.name, rawKeys, proposed, reserved)
			if errors.Is(promptErr, asset.ErrSkip) {
				continue
			}
			if shimNames == nil {
				shimNames = proposed
			}
		}
		bins := make(map[string]string, len(shimNames))
		for i, s := range selected {
			bins[shimNames[i]] = s.Key() // shimName → binKey
		}
		shimPlans = append(shimPlans, shimPlan{
			key:       key,
			jobName:   r.job.name,
			source:    r.job.source,
			pkgDir:    pkgDir,
			bins:      bins,
			pin:       r.job.pin(),
			version:   config.NormalizeVersion(r.release.TagName),
			assetName: r.chosen.Name,
			binDir:    selected[0].BinDir,
		})
	}

	if len(shimPlans) > 0 {
		type shimRow struct{ shim, binary, pkg string }
		var shimRows []shimRow
		for _, p := range shimPlans {
			for shimName, binKey := range p.bins {
				shimRows = append(shimRows, shimRow{shimName, binKey, p.key})
			}
		}
		slices.SortFunc(shimRows, func(a, b shimRow) int { return strings.Compare(a.shim, b.shim) })
		rows := make([][]string, len(shimRows))
		for i, r := range shimRows {
			rows[i] = []string{r.pkg, r.binary, r.shim}
		}
		fmt.Println()
		printTable([]string{"name", "binary", "shim"}, rows, nil)
		fmt.Println()
		if !promptConfirm(fmt.Sprintf("create %d shim(s)", len(shimRows))) {
			if hadErrors {
				return errSilent
			}
			return nil
		}

		for _, p := range shimPlans {
			manifest.Repos[p.jobName] = p.source
			manifest.Extracts[p.key] = config.PackageEntry{
				Pin:       p.pin,
				Version:   p.version,
				AssetName: p.assetName,
				BinDir:    p.binDir,
				Bins:      p.bins,
			}
			for shimName, binsKey := range p.bins {
				binDir, binName := splitBinKey(binsKey)
				if err := shim.Create(shimName, binName, p.pkgDir, binDir); err != nil {
					printWarn(cfg, "%s: could not create shim: %v", shimName, err)
				}
			}
			printPass(cfg, "%s: installed %s", p.jobName, p.version)
		}
	}

	if len(shimPlans) > 0 {
		if err := config.SaveManifest(manifest); err != nil {
			printFail(cfg, "could not save manifest: %v", err)
			return errSilent
		}
	}

	if hadErrors {
		return errSilent
	}
	return nil
}

func parseSourceArg(name string) (source, repoName string, err error) {
	if !strings.Contains(name, "/") {
		return "", "", nil
	}
	src := name
	firstSegment := src[:strings.Index(src, "/")]
	if !strings.Contains(firstSegment, ".") {
		// No dot → org/repo shorthand; github.com implied.
		src = "github.com/" + src
	}
	_, repo, splitErr := gh.SplitSource(src)
	if splitErr != nil {
		return "", "", fmt.Errorf("invalid source %q: must be org/repo or host/org/repo", name)
	}
	return src, repo, nil
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
