package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/asset"
	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/parallel"
	"github.com/meop/ghpm/internal/shim"
)

func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "sync [name...]",
		Aliases: []string{"sy", "up", "update"},
		Short:   "Sync packages to their latest releases",
		RunE:    runSync,
	}
	addSkipVerifyFlag(cmd)
	cmd.Flags().BoolP("force", "f", false, "Reinstall even if already at latest version")
	return cmd
}

func runSync(cmd *cobra.Command, args []string) error {
	forceSync, _ := cmd.Flags().GetBool("force")
	ci, err := initCommand(cmdOptions{Lock: true, Manifest: true, GH: true, NoVerify: true})
	if err != nil {
		return err
	}
	defer ci.close()
	cfg := ci.cfg
	manifest := ci.manifest
	ghClient := ci.gh
	dirs := ci.dirs
	ctx := cmd.Context()

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

	keyToSource := make(map[string]string, len(targets))
	for key := range targets {
		pkgName, _, _ := config.ParseVersionSuffix(key)
		keyToSource[key] = manifest.Repos[pkgName]
	}
	items := buildBatchItems(targets, manifest.Repos)

	batchResults := ghClient.BatchLatestVersions(ctx, items, cfg.CacheTTL)

	type syncJob struct {
		key     string
		source  string
		pkg     config.PackageEntry
		release gh.Release
		chosens []gh.Asset
	}

	type syncTaskResult struct {
		r syncJob
		extractResult
	}

	var ready []syncJob
	checked := 0
	skipped := 0
	var hadErrors bool

	for _, res := range batchResults {
		if res.Err != nil {
			if gh.IsRateLimited(res.Err) {
				skipped++
				printRateLimited(cfg, res.Key)
				continue
			}
			printFail(cfg, "%s: %v", res.Key, res.Err)
			hadErrors = true
			continue
		}
		checked++
		pkg := targets[res.Key]
		latest := config.NormalizeVersion(res.LatestTag)
		if config.CompareVersions(latest, pkg.Version) <= 0 && !forceSync {
			continue
		}
		source := keyToSource[res.Key]
		owner, repo, _ := gh.SplitSource(source)
		rel, err := ghClient.GetReleaseByTag(ctx, owner, repo, res.LatestTag)
		if err != nil {
			printFail(cfg, "%s: %v", res.Key, err)
			hadErrors = true
			continue
		}
		pkgName, _, _ := config.ParseVersionSuffix(res.Key)

		oldAssetNames := make([]string, 0, len(pkg.Asset))
		for a := range pkg.Asset {
			oldAssetNames = append(oldAssetNames, a)
		}
		slices.Sort(oldAssetNames)

		var chosens []gh.Asset
		skippedPkg := false
		for _, assetName := range oldAssetNames {
			ac, acErr := asset.SelectAssetAuto(rel.Assets, cfg, assetName, pkgName)
			if acErr != nil {
				printFail(cfg, "%s: %v", res.Key, acErr)
				hadErrors = true
				skippedPkg = true
				break
			}
			if ac.Chosen.Name == "" {
				sep()
			}
			chosen, chErr := asset.PromptFromCandidates(ac)
			if errors.Is(chErr, asset.ErrSkip) {
				skippedPkg = true
				break
			}
			if chErr != nil {
				printFail(cfg, "%s: %v", res.Key, chErr)
				hadErrors = true
				skippedPkg = true
				break
			}
			chosens = append(chosens, chosen)
		}
		if skippedPkg || len(chosens) == 0 {
			continue
		}
		ready = append(ready, syncJob{key: res.Key, source: source, pkg: pkg, release: rel, chosens: chosens})
	}

	if skipped > 0 {
		printRateLimitSummary(cfg, checked, len(items), skipped)
	}

	if len(ready) == 0 {
		if skipped == 0 {
			print(msgAllUpToDate)
		}
		if hadErrors {
			return errSilent
		}
		return nil
	}

	var rows [][]string
	for _, r := range ready {
		for _, c := range r.chosens {
			rows = append(rows, []string{r.key, r.pkg.Pin, r.pkg.Version, config.NormalizeVersion(r.release.TagName), c.Name, r.source})
		}
	}
	colors := []func(string) string{nil, nil, colorfn(cfg, "old"), colorfn(cfg, "new"), nil, nil}
	printTable([]string{"name", "pin", "version", "update", "asset", "repo"}, rows, colors)

	if dryRun {
		return nil
	}

	sep()
	if !promptConfirm(fmt.Sprintf("update %d package(s)", len(ready))) {
		return nil
	}
	hasOutput = false

	syncTasks := make([]parallel.Task[syncTaskResult], len(ready))
	for i, r := range ready {
		syncTasks[i] = parallel.Task[syncTaskResult]{
			Name: r.key,
			Run: func() (syncTaskResult, error) {
				owner, repo, _ := gh.SplitSource(r.source)
				cacheDir, err := dirs.ReleaseDir(r.source, r.release.TagName)
				if err != nil {
					return syncTaskResult{}, err
				}
				newVersion := config.NormalizeVersion(r.release.TagName)
				pkgName, _, _ := config.ParseVersionSuffix(r.key)
				ex, err := downloadAndExtract(ctx, cfg, ghClient, dirs, owner, repo, r.release.TagName, cacheDir, r.key, r.key, newVersion, pkgName, r.chosens)
				if err != nil {
					return syncTaskResult{}, err
				}
				return syncTaskResult{r: r, extractResult: ex}, nil
			},
		}
	}

	for _, res := range parallel.Run(cmd.Context(), syncTasks, cfg.NumParallel) {
		printTitle(res.Name)
		if res.Err != nil {
			printFail(cfg, "%v", res.Err)
			hadErrors = true
			continue
		}
		tr := res.Value
		newVer := config.NormalizeVersion(tr.r.release.TagName)
		newAssets := make(map[string]config.AssetEntry)
		pkgFailed := false

		if len(tr.binsByAsset) > 0 {
			prevBins := tr.r.pkg.AllBins()
			oldBinKeyToShim := make(map[string]string, len(prevBins))
			for shimName, binsKey := range prevBins {
				oldBinKeyToShim[binsKey] = shimName
			}

			binAssetNames := make([]string, 0, len(tr.binsByAsset))
			for a := range tr.binsByAsset {
				binAssetNames = append(binAssetNames, a)
			}
			slices.Sort(binAssetNames)

			allNewBins := make(map[string]map[string]string)
			selectionFailed := false
			for _, assetName := range binAssetNames {
				prevAssetBinKeys := make([]string, 0, len(tr.r.pkg.Asset[assetName].Bin))
				for _, binKey := range tr.r.pkg.Asset[assetName].Bin {
					prevAssetBinKeys = append(prevAssetBinKeys, binKey)
				}
				selected, discoverErr := asset.SelectBins(tr.binsByAsset[assetName], prevAssetBinKeys)
				if errors.Is(discoverErr, asset.ErrSkip) {
					selectionFailed = true
					break
				}
				if len(selected) == 0 {
					printFail(cfg, msgNoBinaryFound, assetName)
					hadErrors = true
					selectionFailed = true
					break
				}
				for _, s := range selected {
					printInfo(cfg, "bin %s", s.Key())
				}
				newBins := make(map[string]string, len(selected))
				for _, s := range selected {
					if oldShim, ok := oldBinKeyToShim[s.Key()]; ok {
						newBins[oldShim] = s.Key()
					} else {
						newBins[deriveShimName(tr.r.key, s.BinName)] = s.Key()
					}
				}
				allNewBins[assetName] = newBins
			}

			if selectionFailed {
				pkgFailed = true
			} else {
				for shimName := range prevBins {
					_ = shim.Remove(shimName)
				}
				shimFailed := false
				for assetName, newBins := range allNewBins {
					newAssets[assetName] = config.AssetEntry{Bin: newBins}
					for shimName, binsKey := range newBins {
						binDir, binName := parseBinPath(binsKey)
						if err := shim.Create(shimName, binName, tr.pkgDirByAsset[assetName], binDir); err != nil {
							printFail(cfg, "%s: could not update shim: %v", shimName, err)
							shimFailed = true
							hadErrors = true
						}
					}
				}
				if shimFailed {
					pkgFailed = true
				} else {
					printPass(cfg, "updated %s → %s", tr.r.pkg.Version, newVer)
				}
			}
		}

		if len(tr.fontsByAsset) > 0 {
			fontsDir, err := ensureFontDir()
			fontFailed := err != nil
			if err != nil {
				printFail(cfg, "font dir: %v", err)
				hadErrors = true
				pkgFailed = true
			}

			if !fontFailed {
				allFonts := tr.r.pkg.AllFonts()
				oldPathToName := make(map[string]string)
				for fontName, fontPath := range allFonts {
					oldPathToName[fontPath] = fontName
				}

				fontAssetNames := make([]string, 0, len(tr.fontsByAsset))
				for a := range tr.fontsByAsset {
					fontAssetNames = append(fontAssetNames, a)
				}
				slices.Sort(fontAssetNames)

				for _, assetName := range fontAssetNames {
					candidates := tr.fontsByAsset[assetName]
					prevAssetPaths := make([]string, 0, len(tr.r.pkg.Asset[assetName].Font))
					for _, fontPath := range tr.r.pkg.Asset[assetName].Font {
						prevAssetPaths = append(prevAssetPaths, fontPath)
					}
					selectedFonts, selErr := asset.SelectFonts(candidates, prevAssetPaths)
					if errors.Is(selErr, asset.ErrSkip) {
						continue
					}
					if selErr != nil {
						printFail(cfg, "%v", selErr)
						hadErrors = true
						pkgFailed = true
						continue
					}
					fontMap := make(map[string]string)
					for _, sel := range selectedFonts {
						fontPath := sel.Key()
						srcPath := filepath.Join(tr.pkgDirByAsset[assetName], filepath.FromSlash(fontPath))
						if err := installFont(srcPath, fontsDir); err != nil {
							printFail(cfg, "font %s: %v", fontPath, err)
							hadErrors = true
							pkgFailed = true
							continue
						}
						fontName, hasName := oldPathToName[fontPath]
						if !hasName {
							fontName = asset.DeriveFontName(sel.FontName)
						}
						fontMap[fontName] = fontPath
					}
					if len(fontMap) > 0 {
						newAssets[assetName] = config.AssetEntry{Font: fontMap}
					}
				}

				for _, fontPath := range allFonts {
					uninstallFont(fontPath, fontsDir)
				}
				if !pkgFailed {
					printPass(cfg, "updated %s → %s", tr.r.pkg.Version, newVer)
				}
			}
		}

		if !pkgFailed && tr.r.pkg.Version != newVer {
			if oldBase, err := dirs.ExtractBaseDir(tr.r.key); err == nil {
				if err := os.RemoveAll(filepath.Join(oldBase, tr.r.pkg.Version)); err != nil {
					printWarn(cfg, "could not remove old extract dir: %v", err)
				}
			}
		}

		if !pkgFailed && len(newAssets) > 0 {
			manifest.Extracts[tr.r.key] = config.PackageEntry{
				Pin:     tr.r.pkg.Pin,
				Version: newVer,
				Asset:   newAssets,
			}
		}
	}

	if err := saveManifest(cfg, manifest); err != nil {
		return err
	}

	if hadErrors {
		return errSilent
	}
	return nil
}
