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
	addSkipHashCheckFlag(cmd)
	cmd.Flags().BoolP("force", "f", false, "Reinstall even if already at latest version")
	return cmd
}

func runSync(cmd *cobra.Command, args []string) error {
	forceSync, _ := cmd.Flags().GetBool("force")
	ci, err := initCommand(cmdOptions{Lock: true, Manifest: true, GH: true, SkipHashCheck: true})
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
			matched := filterExtracts(manifest.Extracts, []string{name})
			if len(matched) == 0 {
				printInfo(cfg, "%s: not installed", name)
				continue
			}
			for key, p := range matched {
				if p.Pin == "fixed" {
					printInfo(cfg, "%s: fixed at %s, skipping", key, p.Version)
					continue
				}
				targets[key] = p
			}
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
			rows = append(rows, []string{r.key, r.pkg.Version, config.NormalizeVersion(r.release.TagName), r.pkg.Pin, r.source, c.Name})
		}
	}
	colors := []func(string) string{nil, colorfn(cfg, "old"), colorfn(cfg, "new"), nil, nil, nil}
	printTable([]string{"name", "version", "update", "pin", "repo", "asset"}, rows, colors)

	if dryRun {
		return nil
	}

	if !promptConfirm(fmt.Sprintf("update %d package(s)", len(ready))) {
		return nil
	}

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

	successCount := 0
	for _, res := range parallel.Run(cmd.Context(), syncTasks, cfg.NumParallel) {
		if res.Err != nil {
			printFail(cfg, "%s: %v", res.Name, res.Err)
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
				selected, discoverErr := asset.SelectBins(tr.binsByAsset[assetName], prevAssetBinKeys, res.Name)
				if errors.Is(discoverErr, asset.ErrSkip) {
					selectionFailed = true
					break
				}
				if len(selected) == 0 {
					printFail(cfg, "%s: no binary found in %s", res.Name, assetName)
					hadErrors = true
					selectionFailed = true
					break
				}
				for _, s := range selected {
					printInfo(cfg, "%s: bin %s", res.Name, s.Key())
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

			if !selectionFailed {
				pkgBase, _, _ := config.ParseVersionSuffix(tr.r.key)
				reserved := reservedShimNames(manifest, pkgBase)
				var rawKeys, proposed []string
				type binPos struct{ asset, shimName string }
				var positions []binPos
				for _, assetName := range binAssetNames {
					bins := allNewBins[assetName]
					binKeysList := make([]string, 0, len(bins))
					for _, bk := range bins {
						binKeysList = append(binKeysList, bk)
					}
					slices.Sort(binKeysList)
					shimByKey := make(map[string]string, len(bins))
					for sh, bk := range bins {
						shimByKey[bk] = sh
					}
					for _, bk := range binKeysList {
						rawKeys = append(rawKeys, bk)
						proposed = append(proposed, shimByKey[bk])
						positions = append(positions, binPos{assetName, shimByKey[bk]})
					}
				}
				if hasReservedConflict(proposed, reserved) {
					renamed, promptErr := asset.PromptBinNames(rawKeys, proposed, reserved, res.Name)
					if errors.Is(promptErr, asset.ErrSkip) {
						selectionFailed = true
					} else if renamed != nil {
						for i, pos := range positions {
							if renamed[i] != pos.shimName {
								allNewBins[pos.asset][renamed[i]] = allNewBins[pos.asset][pos.shimName]
								delete(allNewBins[pos.asset], pos.shimName)
							}
						}
					}
				}
			}

			if selectionFailed {
				pkgFailed = true
			} else {
				for shimName := range prevBins {
					_ = shim.Remove(shimName)
				}
				shimFailed := false
				for assetName, newBins := range allNewBins {
					ae := newAssets[assetName]
					ae.Bin = newBins
					newAssets[assetName] = ae
					for shimName, binsKey := range newBins {
						binDir, binName := parseBinPath(binsKey)
						if err := shim.Create(shimName, binName, tr.pkgDirByAsset[assetName], binDir); err != nil {
							printFail(cfg, "%s: %s: could not update shim: %v", res.Name, shimName, err)
							shimFailed = true
							hadErrors = true
						}
					}
				}
				if shimFailed {
					pkgFailed = true
				}
			}
		}

		if len(tr.fontsByAsset) > 0 {
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

			// Phase 1: select fonts and pre-compute names (preserve old, derive new).
			type syncFontAsset struct {
				assetName string
				fontMap   map[string]string // fontName → fontPath
			}
			var pendingFonts []syncFontAsset
			for _, assetName := range fontAssetNames {
				candidates := tr.fontsByAsset[assetName]
				prevAssetPaths := make([]string, 0, len(tr.r.pkg.Asset[assetName].Font))
				for _, fontPath := range tr.r.pkg.Asset[assetName].Font {
					prevAssetPaths = append(prevAssetPaths, fontPath)
				}
				selectedFonts, selErr := asset.SelectFonts(candidates, prevAssetPaths, res.Name)
				if errors.Is(selErr, asset.ErrSkip) {
					continue
				}
				if selErr != nil {
					printFail(cfg, "%s: %v", res.Name, selErr)
					hadErrors = true
					pkgFailed = true
					continue
				}
				fontMap := make(map[string]string, len(selectedFonts))
				for _, sel := range selectedFonts {
					fontPath := sel.Key()
					fontName, hasName := oldPathToName[fontPath]
					if !hasName {
						fontName = asset.DeriveFontName(sel.FontName)
					}
					fontMap[fontName] = fontPath
				}
				pendingFonts = append(pendingFonts, syncFontAsset{assetName, fontMap})
			}

			// Phase 2: conflict check across all pending font names.
			if !pkgFailed && len(pendingFonts) > 0 {
				pkgBase, _, _ := config.ParseVersionSuffix(tr.r.key)
				fontReserved := reservedFontNames(manifest, pkgBase)
				var fontKeys, proposed []string
				type fontPos struct {
					assetIdx int
					name     string
				}
				var positions []fontPos
				for i, pf := range pendingFonts {
					pathsSorted := make([]string, 0, len(pf.fontMap))
					for _, fp := range pf.fontMap {
						pathsSorted = append(pathsSorted, fp)
					}
					slices.Sort(pathsSorted)
					pathToName := make(map[string]string, len(pf.fontMap))
					for name, fp := range pf.fontMap {
						pathToName[fp] = name
					}
					for _, fp := range pathsSorted {
						fontKeys = append(fontKeys, fp)
						proposed = append(proposed, pathToName[fp])
						positions = append(positions, fontPos{i, pathToName[fp]})
					}
				}
				if hasReservedConflict(proposed, fontReserved) {
					renamed, promptErr := asset.PromptFontConflicts(fontKeys, proposed, fontReserved, res.Name)
					if errors.Is(promptErr, asset.ErrSkip) {
						pkgFailed = true
					} else if renamed != nil {
						for i, pos := range positions {
							if renamed[i] != pos.name {
								pf := &pendingFonts[pos.assetIdx]
								pf.fontMap[renamed[i]] = pf.fontMap[pos.name]
								delete(pf.fontMap, pos.name)
							}
						}
					}
				}
			}

			// Phase 3: install fonts.
			if !pkgFailed {
				fontsDir, err := ensureFontDir()
				if err != nil {
					printFail(cfg, "%s: font dir: %v", res.Name, err)
					hadErrors = true
					pkgFailed = true
				} else {
					for _, pf := range pendingFonts {
						fontMap := make(map[string]string, len(pf.fontMap))
						for fontName, fontPath := range pf.fontMap {
							srcPath := filepath.Join(tr.pkgDirByAsset[pf.assetName], filepath.FromSlash(fontPath))
							if err := installFont(srcPath, fontsDir); err != nil {
								printFail(cfg, "%s: %s: could not install font: %v", res.Name, fontName, err)
								hadErrors = true
								pkgFailed = true
								continue
							}
							fontMap[fontName] = fontPath
						}
						if len(fontMap) > 0 {
							ae := newAssets[pf.assetName]
							ae.Font = fontMap
							newAssets[pf.assetName] = ae
						}
					}
					var newPaths []string
					for _, pf := range pendingFonts {
						for _, fontPath := range pf.fontMap {
							newPaths = append(newPaths, fontPath)
						}
					}
					for _, fontPath := range staleFontPaths(allFonts, newPaths) {
						uninstallFont(fontPath, fontsDir)
					}
				}
			}
		}

		if !pkgFailed && (len(tr.binsByAsset) > 0 || len(tr.fontsByAsset) > 0) {
			successCount++
		}

		if !pkgFailed && tr.r.pkg.Version != newVer {
			if oldBase, err := dirs.ExtractBaseDir(tr.r.key); err == nil {
				if err := os.RemoveAll(filepath.Join(oldBase, tr.r.pkg.Version)); err != nil {
					printWarn(cfg, "%s: could not remove old extract dir: %v", res.Name, err)
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

	if successCount > 0 {
		printPass(cfg, "updated %d package(s)", successCount)
	}

	if err := saveManifest(cfg, manifest); err != nil {
		return err
	}

	if hadErrors {
		return errSilent
	}
	return nil
}
