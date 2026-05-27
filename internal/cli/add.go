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
	ci, err := initCommand(cmdOptions{Lock: true, Manifest: true, GH: true, Dirs: true, Repos: true, NoVerify: true})
	if err != nil {
		return err
	}
	defer ci.close()
	cfg := ci.cfg
	manifest := ci.manifest
	repos := ci.repos
	ctx := cmd.Context()

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

		printTitle(name)

		if name == binGhpm || name == binGh {
			printInfo(cfg, "self managed, skipping")
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
				var err error
				source, err = config.SearchGitHub(name)
				if err != nil {
					printFail(cfg, "%v", err)
					hadErrors = true
					continue
				}
			}
			printInfo(cfg, "repo: %s", source)
		}

		jobKey := name
		if pinned {
			jobKey = name + "@" + strings.TrimPrefix(ver, "v")
		}
		if entry, exists := manifest.Extracts[jobKey]; exists && !forceInstall {
			printInfo(cfg, "already installed %s", entry.Version)
			continue
		}
		if !pinned {
			if existing, found := config.FindBySource(source, manifest); found && existing != name && !forceInstall {
				printInfo(cfg, "already installed as %s — skipping", existing)
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
			rel, err = gh.GetLatestRelease(ctx, owner, repo)
		} else {
			c, perr := config.ParseConstraint(ver)
			if perr != nil {
				printFail(cfg, "%v", perr)
				hadErrors = true
				continue
			}
			if c.Level == config.PinExact {
				rel, err = gh.GetReleaseByTag(ctx, owner, repo, ver)
			} else {
				rel, err = gh.FindLatestMatching(ctx, owner, repo, c)
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
		if ac.Chosen.Name == "" {
			sep()
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
			fmt.Printf("%s: install %s (asset: %s)\n", r.job.name, config.NormalizeVersion(r.release.TagName), r.chosen.Name)
		}
		return nil
	}

	if !promptInstall(cfg, ready) {
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
					if err := gh.DownloadAsset(ctx, owner, repo, r.release.TagName, r.chosen.Name, cacheDir); err != nil {
						return nil, err
					}
				}
				if !noVerify {
					_, err := gh.VerifyAsset(ctx, owner, repo, r.release.TagName, cacheDir, r.chosen.Name)
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
		key     string
		jobName string
		source  string
		pkgDir  string
		bins    map[string]string
		pin     string
		version string
		asset   string
	}
	var shimPlans []shimPlan

	for _, res := range installResults {
		printTitle(res.Name)
		if res.Err != nil {
			printFail(cfg, "%v", res.Err)
			hadErrors = true
			continue
		}
		r, ok := res.Value.(jobWithRelease)
		if !ok {
			continue
		}
		pkgDir, _ := store.ExtractDir(r.job.key(), config.NormalizeVersion(r.release.TagName))
		candidates := asset.FindBinaries(pkgDir, r.job.name)
		selected, discoverErr := asset.SelectBinaries(candidates, nil)
		if errors.Is(discoverErr, asset.ErrSkip) {
			continue
		}
		if len(selected) == 0 {
			printFail(cfg, "no binary found in %s", r.chosen.Name)
			hadErrors = true
			continue
		}
		rawKeys := make([]string, len(selected))
		for i, s := range selected {
			rawKeys[i] = s.Key()
		}
		printInfo(cfg, "bin %s", strings.Join(rawKeys, ", "))
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
			sep()
			var promptErr error
			shimNames, promptErr = asset.PromptShimRenames(rawKeys, proposed, reserved)
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
			key:     key,
			jobName: r.job.name,
			source:  r.job.source,
			pkgDir:  pkgDir,
			bins:    bins,
			pin:     r.job.pin(),
			version: config.NormalizeVersion(r.release.TagName),
			asset:   r.chosen.Name,
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
		printTable([]string{"name", "bin", "target"}, rows, nil)
		sep()
		if !promptConfirm(fmt.Sprintf("create %d bin(s)", len(shimRows))) {
			if hadErrors {
				return errSilent
			}
			return nil
		}

		for _, p := range shimPlans {
			if forceInstall {
				if existing, ok := manifest.Extracts[p.key]; ok {
					for shimName := range existing.Bins {
						_ = shim.Remove(shimName)
					}
				}
			}
			manifest.Repos[p.jobName] = p.source
			manifest.Extracts[p.key] = config.PackageEntry{
				Pin:     p.pin,
				Version: p.version,
				Asset:   p.asset,
				Bins:    p.bins,
			}
			shimFailed := false
			for shimName, binsKey := range p.bins {
				binDir, binName := splitBinKey(binsKey)
				if err := shim.Create(shimName, binName, p.pkgDir, binDir); err != nil {
					printFail(cfg, "%s: could not create shim: %v", shimName, err)
					shimFailed = true
					hadErrors = true
				}
			}
			if !shimFailed {
				printPass(cfg, "%s: installed %s", p.jobName, p.version)
			}
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

func promptInstall(cfg *config.Settings, ready []jobWithRelease) bool {
	rows := make([][]string, len(ready))
	for i, r := range ready {
		rows[i] = []string{r.job.key(), r.job.pin(), config.NormalizeVersion(r.release.TagName), r.chosen.Name, r.job.source}
	}
	colors := []func(string) string{nil, nil, colorfn(cfg, "new"), nil, nil}
	printTable([]string{"name", "pin", "update", "asset", "repo"}, rows, colors)
	sep()
	return promptConfirm(fmt.Sprintf("install %d package(s)", len(ready)))
}
