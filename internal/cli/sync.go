package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/asset"
	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/parallel"
	"github.com/meop/ghpm/internal/shim"
	"github.com/meop/ghpm/internal/store"
)

func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "sync [name...]",
		Aliases: []string{"up", "update"},
		Short:   "Sync packages to their latest releases",
		RunE:    runSync,
	}
	cmd.Flags().BoolVarP(&noVerify, "skip-verify", "s", false, "Skip SHA256 verification")
	return cmd
}

func runSync(cmd *cobra.Command, args []string) error {
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
				printInfo(cfg, "%s: not installed", name)
				continue
			}
			if p.Pin == "fixed" {
				printInfo(cfg, "%s: fixed at %s, skipping", name, p.Version)
				continue
			}
			targets[name] = p
		}
	}
	if len(targets) == 0 {
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

	type syncJob struct {
		key     string
		source  string
		pkg     config.PackageEntry
		release gh.Release
		chosen  gh.Asset
	}

	var ready []syncJob
	checked := 0
	skipped := 0
	var hadErrors bool

	for _, res := range batchResults {
		if res.Err != nil {
			if gh.IsRateLimited(res.Err) {
				skipped++
				printWarn(cfg, "%s: rate limited", res.Key)
				continue
			}
			printFail(cfg, "%s: %v", res.Key, res.Err)
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
			printFail(cfg, "%s: %v", res.Key, err)
			hadErrors = true
			continue
		}
		pkg = targets[res.Key]
		ac, err := asset.SelectAssetAuto(rel.Assets, cfg, pkg.AssetName, res.Key)
		if err != nil {
			printFail(cfg, "%s: %v", res.Key, err)
			hadErrors = true
			continue
		}
		chosen, err := asset.PromptFromCandidates(ac)
		if errors.Is(err, asset.ErrSkip) {
			continue
		}
		if err != nil {
			printFail(cfg, "%s: %v", res.Key, err)
			hadErrors = true
			continue
		}
		if ac.Chosen.Name != "" {
			printInfo(cfg, "asset: %s", chosen.Name)
		}
		job := syncJob{key: res.Key, source: items[0].Source, pkg: pkg, release: rel, chosen: chosen}
		for _, it := range items {
			if it.Key == res.Key {
				job.source = it.Source
				break
			}
		}
		ready = append(ready, job)
	}

	if skipped > 0 {
		printWarn(cfg, "checked %d/%d packages (%d skipped due to rate limiting)", checked, len(items), skipped)
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

	rows := make([][]string, len(ready))
	for i, r := range ready {
		rows[i] = []string{r.key, r.pkg.Pin, r.pkg.Version, config.NormalizeVersion(r.release.TagName), r.chosen.Name, r.source}
	}
	colors := []func(string) string{nil, nil, colorfn(cfg, "old"), colorfn(cfg, "new"), nil, nil}
	printTable([]string{"name", "pin", "version", "update", "asset", "repo"}, rows, colors)

	if dryRun {
		return nil
	}

	fmt.Println()
	if !promptConfirm(fmt.Sprintf("update %d package(s)", len(ready))) {
		return nil
	}

	syncTasks := make([]parallel.Task, len(ready))
	for i, r := range ready {
		syncTasks[i] = parallel.Task{
			Name: r.key,
			Run: func() (any, error) {
				owner, repo, _ := gh.SplitSource(r.source)
				cacheDir, err := store.ReleaseDir(r.source, r.release.TagName)
				if err != nil {
					return nil, err
				}
				if _, err := os.Stat(filepath.Join(cacheDir, r.chosen.Name)); os.IsNotExist(err) {
					printInfo(cfg, "%s: downloading %s...", r.key, config.NormalizeVersion(r.release.TagName))
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
				newVersion := config.NormalizeVersion(r.release.TagName)
				newPkgDir, err := store.ExtractDir(r.key, newVersion)
				if err != nil {
					return nil, err
				}
				if err := os.RemoveAll(newPkgDir); err != nil {
					return nil, err
				}
				if err := asset.ExtractPackage(cacheDir, r.chosen.Name, newPkgDir); err != nil {
					_ = os.RemoveAll(newPkgDir)
					return nil, err
				}
				return r, nil
			},
		}
	}

	for i, res := range parallel.Run(cmd.Context(), syncTasks, cfg.NumParallel) {
		if i > 0 {
			fmt.Println()
		}
		if res.Err != nil {
			printFail(cfg, "%s: %v", res.Name, res.Err)
			hadErrors = true
			continue
		}
		r, ok := res.Value.(syncJob)
		if !ok {
			continue
		}
		newVer := config.NormalizeVersion(r.release.TagName)
		newPkgDir, _ := store.ExtractDir(r.key, newVer)
		name, _, _ := config.ParseVersionSuffix(r.key)
		prevBinKeys := make([]string, 0, len(r.pkg.Bins))
		for _, binsKey := range r.pkg.Bins { // values are the relative binary paths
			prevBinKeys = append(prevBinKeys, binsKey)
		}
		candidates := asset.FindBinaries(newPkgDir, name)
		selected, discoverErr := asset.SelectBinaries(candidates, name, prevBinKeys)
		if errors.Is(discoverErr, asset.ErrSkip) {
			continue
		}
		if len(selected) == 0 {
			printFail(cfg, "%s: no binary found in %s", r.key, r.chosen.Name)
			hadErrors = true
			continue
		}
		rawKeys := make([]string, len(selected))
		for i, s := range selected {
			rawKeys[i] = s.Key()
		}
		printInfo(cfg, "%s: binary %s", r.key, strings.Join(rawKeys, ", "))
		for shimName := range r.pkg.Bins {
			_ = shim.Remove(shimName)
		}
		if oldBase, err := store.ExtractBaseDir(r.key); err == nil {
			oldPkgDir := filepath.Join(oldBase, r.pkg.Version)
			if err := os.RemoveAll(oldPkgDir); err != nil {
				printWarn(cfg, "%s: could not remove old extract dir: %v", r.key, err)
			}
		}
		// Carry over custom shim names: build reverse map binKey → shimName from old entry.
		oldBinKeyToShim := make(map[string]string, len(r.pkg.Bins))
		for shimName, binsKey := range r.pkg.Bins {
			oldBinKeyToShim[binsKey] = shimName
		}
		newBins := make(map[string]string, len(selected))
		for _, s := range selected {
			if oldShim, ok := oldBinKeyToShim[s.Key()]; ok {
				newBins[oldShim] = s.Key() // preserve custom shim name
			} else {
				newBins[binShimName(r.key, s.BinName)] = s.Key() // default for new binary
			}
		}
		manifest.Extracts[r.key] = config.PackageEntry{
			Pin:       r.pkg.Pin,
			Version:   newVer,
			AssetName: r.chosen.Name,
			BinDir:    selected[0].BinDir,
			Bins:      newBins,
		}
		for shimName, binsKey := range newBins {
			binDir, binName := splitBinKey(binsKey)
			if err := shim.Create(shimName, binName, newPkgDir, binDir); err != nil {
				printWarn(cfg, "%s: could not update shim: %v", shimName, err)
			}
		}
		printPass(cfg, "%s: updated %s → %s", r.key, r.pkg.Version, newVer)
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
