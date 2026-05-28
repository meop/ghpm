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

var forceSync bool

func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "sync [name...]",
		Aliases: []string{"sy", "up", "update"},
		Short:   "Sync packages to their latest releases",
		RunE:    runSync,
	}
	cmd.Flags().BoolVarP(&noVerify, "skip-verify", "s", false, "Skip SHA256 verification")
	cmd.Flags().BoolVarP(&forceSync, "force", "f", false, "Reinstall even if already at latest version")
	return cmd
}

func runSync(cmd *cobra.Command, args []string) error {
	ci, err := initCommand(cmdOptions{Lock: true, Manifest: true, GH: true, NoVerify: true})
	if err != nil {
		return err
	}
	defer ci.close()
	cfg := ci.cfg
	manifest := ci.manifest
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
	items := make([]gh.BatchItem, 0, len(targets))
	for key := range targets {
		name, verStr, isPinned := config.ParseVersionSuffix(key)
		source := manifest.Repos[name]
		keyToSource[key] = source
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

	batchResults := gh.BatchLatestVersions(ctx, items, cfg.CacheTTL)

	type syncJob struct {
		key     string
		source  string
		pkg     config.PackageEntry
		release gh.Release
		chosens []gh.Asset
	}

	type syncTaskResult struct {
		r            syncJob
		pkgDir       string
		fontsByAsset map[string][]asset.FontCandidate
		binsByAsset  map[string][]asset.BinaryCandidate
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
		if config.CompareVersions(latest, pkg.Version) <= 0 && !forceSync {
			continue
		}
		source := keyToSource[res.Key]
		owner, repo, _ := gh.SplitSource(source)
		rel, err := gh.GetReleaseByTag(ctx, owner, repo, res.LatestTag)
		if err != nil {
			printFail(cfg, "%s: %v", res.Key, err)
			hadErrors = true
			continue
		}
		name, _, _ := config.ParseVersionSuffix(res.Key)

		oldAssetNames := make([]string, 0, len(pkg.Asset))
		for a := range pkg.Asset {
			oldAssetNames = append(oldAssetNames, a)
		}
		slices.Sort(oldAssetNames)

		var chosens []gh.Asset
		skippedPkg := false
		for _, assetName := range oldAssetNames {
			ac, acErr := asset.SelectAssetAuto(rel.Assets, cfg, assetName, name)
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
		printWarn(cfg, "checked %d/%d packages (%d skipped due to rate limiting)", checked, len(items), skipped)
	}

	if len(ready) == 0 {
		if skipped == 0 {
			print("all packages are up to date")
		}
		if hadErrors {
			return errSilent
		}
		return nil
	}

	rows := make([][]string, len(ready))
	for i, r := range ready {
		assetNames := make([]string, len(r.chosens))
		for j, c := range r.chosens {
			assetNames[j] = c.Name
		}
		rows[i] = []string{r.key, r.pkg.Pin, r.pkg.Version, config.NormalizeVersion(r.release.TagName), strings.Join(assetNames, ", "), r.source}
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
				newVersion := config.NormalizeVersion(r.release.TagName)
				newPkgDir, err := store.ExtractDir(r.key, newVersion)
				if err != nil {
					return nil, err
				}
				if err := os.RemoveAll(newPkgDir); err != nil {
					return nil, err
				}
				if err := os.MkdirAll(newPkgDir, 0755); err != nil {
					return nil, err
				}

				pkgName, _, _ := config.ParseVersionSuffix(r.key)
				fontsByAsset := make(map[string][]asset.FontCandidate)
				binsByAsset := make(map[string][]asset.BinaryCandidate)

				for _, chosen := range r.chosens {
					if _, err := os.Stat(filepath.Join(cacheDir, chosen.Name)); os.IsNotExist(err) {
						printInfo(cfg, "%s: downloading %s...", r.key, chosen.Name)
						if err := gh.DownloadAsset(ctx, owner, repo, r.release.TagName, chosen.Name, cacheDir); err != nil {
							return nil, err
						}
					}
					if !noVerify {
						if _, err := gh.VerifyAsset(ctx, owner, repo, r.release.TagName, cacheDir, chosen.Name); err != nil {
							return nil, err
						}
					}

					prevFonts := fontKeySet(asset.FindFonts(newPkgDir))
					prevBins := binKeySet(asset.FindBinaries(newPkgDir, pkgName))

					if err := asset.ExtractPackage(cacheDir, chosen.Name, newPkgDir); err != nil {
						_ = os.RemoveAll(newPkgDir)
						return nil, err
					}

					if newFonts := filterNewFonts(asset.FindFonts(newPkgDir), prevFonts); len(newFonts) > 0 {
						fontsByAsset[chosen.Name] = newFonts
					}
					if newBins := filterNewBins(asset.FindBinaries(newPkgDir, pkgName), prevBins); len(newBins) > 0 {
						binsByAsset[chosen.Name] = newBins
					}
				}

				return syncTaskResult{r: r, pkgDir: newPkgDir, fontsByAsset: fontsByAsset, binsByAsset: binsByAsset}, nil
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
		tr, ok := res.Value.(syncTaskResult)
		if !ok {
			continue
		}
		newVer := config.NormalizeVersion(tr.r.release.TagName)
		newAssets := make(map[string]config.AssetEntry)
		pkgFailed := false

		if len(tr.binsByAsset) > 0 {
			var allBinCandidates []asset.BinaryCandidate
			var binAssetName string
			for assetName, candidates := range tr.binsByAsset {
				if binAssetName == "" {
					binAssetName = assetName
				}
				allBinCandidates = append(allBinCandidates, candidates...)
			}

			prevBins := tr.r.pkg.AllBins()
			prevBinKeys := make([]string, 0, len(prevBins))
			for _, binsKey := range prevBins {
				prevBinKeys = append(prevBinKeys, binsKey)
			}

			selected, discoverErr := asset.SelectBinaries(allBinCandidates, prevBinKeys)
			if errors.Is(discoverErr, asset.ErrSkip) {
				continue
			}
			if len(selected) == 0 {
				printFail(cfg, "no binary found in %s", binAssetName)
				hadErrors = true
				pkgFailed = true
			} else {
				rawKeys := make([]string, len(selected))
				for i, s := range selected {
					rawKeys[i] = s.Key()
				}
				printInfo(cfg, "bin %s", strings.Join(rawKeys, ", "))

				for shimName := range prevBins {
					_ = shim.Remove(shimName)
				}
				if oldBase, err := store.ExtractBaseDir(tr.r.key); err == nil {
					oldPkgDir := filepath.Join(oldBase, tr.r.pkg.Version)
					if oldPkgDir != tr.pkgDir {
						if err := os.RemoveAll(oldPkgDir); err != nil {
							printWarn(cfg, "could not remove old extract dir: %v", err)
						}
					}
				}

				oldBinKeyToShim := make(map[string]string, len(prevBins))
				for shimName, binsKey := range prevBins {
					oldBinKeyToShim[binsKey] = shimName
				}
				newBins := make(map[string]string, len(selected))
				for _, s := range selected {
					if oldShim, ok := oldBinKeyToShim[s.Key()]; ok {
						newBins[oldShim] = s.Key()
					} else {
						newBins[binShimName(tr.r.key, s.BinName)] = s.Key()
					}
				}
				newAssets[binAssetName] = config.AssetEntry{Bin: newBins}

				shimFailed := false
				for shimName, binsKey := range newBins {
					binDir, binName := splitBinKey(binsKey)
					if err := shim.Create(shimName, binName, tr.pkgDir, binDir); err != nil {
						printFail(cfg, "%s: could not update shim: %v", shimName, err)
						shimFailed = true
						hadErrors = true
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
			fontsDir, err := userFontDir()
			fontFailed := err != nil
			if err != nil {
				printFail(cfg, "font dir: %v", err)
				hadErrors = true
				pkgFailed = true
			} else if err := os.MkdirAll(fontsDir, 0755); err != nil {
				printFail(cfg, "font dir: %v", err)
				fontFailed = true
				hadErrors = true
				pkgFailed = true
			}

			if !fontFailed {
				oldPathToName := make(map[string]string)
				for fontName, fontPath := range tr.r.pkg.AllFonts() {
					oldPathToName[fontPath] = fontName
				}
				prevFontPaths := make([]string, 0, len(tr.r.pkg.AllFonts()))
				for _, fontPath := range tr.r.pkg.AllFonts() {
					prevFontPaths = append(prevFontPaths, fontPath)
				}

				fontAssetNames := make([]string, 0, len(tr.fontsByAsset))
				for a := range tr.fontsByAsset {
					fontAssetNames = append(fontAssetNames, a)
				}
				slices.Sort(fontAssetNames)

				for _, assetName := range fontAssetNames {
					candidates := tr.fontsByAsset[assetName]
					selectedFonts, selErr := asset.SelectFonts(candidates, prevFontPaths)
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
						srcPath := filepath.Join(tr.pkgDir, filepath.FromSlash(fontPath))
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

				for _, fontPath := range tr.r.pkg.AllFonts() {
					uninstallFont(fontPath, fontsDir)
				}
				if oldBase, err := store.ExtractBaseDir(tr.r.key); err == nil {
					oldPkgDir := filepath.Join(oldBase, tr.r.pkg.Version)
					if oldPkgDir != tr.pkgDir {
						_ = os.RemoveAll(oldPkgDir)
					}
				}
				if !pkgFailed {
					printPass(cfg, "updated %s → %s", tr.r.pkg.Version, newVer)
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

	if err := config.SaveManifest(manifest); err != nil {
		printFail(cfg, "could not save manifest: %v", err)
		return errSilent
	}

	if hadErrors {
		return errSilent
	}
	return nil
}
