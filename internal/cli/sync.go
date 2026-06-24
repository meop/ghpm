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
				print("%s: not installed", name)
				continue
			}
			for key, p := range matched {
				if p.Pin == "fixed" {
					print("%s: fixed at %s, skipping", key, p.Version)
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

	// Phase 1: determine what is outdated. This needs only the batched version
	// check — no release fetch, no asset prompts — so the gate table and its
	// confirm come before any of that work is spent. The user can bail here.
	type outdatedPkg struct {
		key       string
		source    string
		pkg       config.PackageEntry
		latestTag string
	}
	var outdated []outdatedPkg
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
		outdated = append(outdated, outdatedPkg{key: res.Key, source: keyToSource[res.Key], pkg: pkg, latestTag: res.LatestTag})
	}

	if skipped > 0 {
		printRateLimitSummary(cfg, checked, len(items), skipped)
	}

	if len(outdated) == 0 {
		if skipped == 0 {
			print(msgAllUpToDate)
		}
		if hadErrors {
			return errSilent
		}
		return nil
	}

	slices.SortFunc(outdated, func(a, b outdatedPkg) int { return strings.Compare(a.key, b.key) })

	updateColors := []func(string) string{nil, colorfn(cfg, "old"), colorfn(cfg, "new"), nil, nil}
	gateRows := make([][]string, 0, len(outdated))
	for _, o := range outdated {
		gateRows = append(gateRows, []string{o.key, o.pkg.Version, config.NormalizeVersion(o.latestTag), o.pkg.Pin, o.source})
	}
	if !gate([]string{"name", "version", "update", "pin", "repo"}, gateRows, updateColors, fmt.Sprintf("update %d package(s)", len(outdated))) {
		return nil
	}

	// Phase 2: after the user opts in, fetch each release and resolve its
	// asset(s), prompting only where the choice is ambiguous. A skipped package
	// drops out here and simply never reaches the final table.
	var ready []syncJob
	for _, o := range outdated {
		owner, repo, _ := gh.SplitSource(o.source)
		rel, err := ghClient.GetReleaseByTag(ctx, owner, repo, o.latestTag)
		if err != nil {
			printFail(cfg, "%s: %v", o.key, err)
			hadErrors = true
			continue
		}
		pkgName, _, _ := config.ParseVersionSuffix(o.key)

		oldAssetNames := o.pkg.Assets

		var chosens []gh.Asset
		skippedPkg := false
		for _, assetName := range oldAssetNames {
			ac, acErr := asset.SelectAssetAuto(rel.Assets, cfg, assetName, pkgName)
			if acErr != nil {
				printFail(cfg, "%s: %v", o.key, acErr)
				hadErrors = true
				skippedPkg = true
				break
			}
			chosen, chErr := asset.PromptFromCandidates(ac, o.key)
			if errors.Is(chErr, asset.ErrSkip) {
				skippedPkg = true
				break
			}
			if chErr != nil {
				printFail(cfg, "%s: %v", o.key, chErr)
				hadErrors = true
				skippedPkg = true
				break
			}
			chosens = append(chosens, chosen)
		}
		if skippedPkg || len(chosens) == 0 {
			continue
		}
		ready = append(ready, syncJob{key: o.key, source: o.source, pkg: o.pkg, release: rel, chosens: chosens})
	}

	if len(ready) == 0 {
		if hadErrors {
			return errSilent
		}
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
				ex, err := downloadAndExtract(ctx, ghClient, dirs, owner, repo, r.release.TagName, cacheDir, r.key, r.key, newVersion, r.chosens)
				if err != nil {
					return syncTaskResult{}, err
				}
				return syncTaskResult{r: r, extractResult: ex}, nil
			},
		}
	}

	updated := 0
	for _, res := range parallel.Run(cmd.Context(), syncTasks, cfg.NumParallel) {
		if res.Err != nil {
			printFail(cfg, "%s: %v", res.Name, res.Err)
			hadErrors = true
			continue
		}
		tr := res.Value
		newVer := config.NormalizeVersion(tr.r.release.TagName)
		pkgFailed := false
		var newBin, newFont map[string]string

		// Bins are discovered across the whole overlay; preserve each previously
		// chosen shim name where the binary path is unchanged, else derive one.
		if len(tr.bins) > 0 {
			prevBins := tr.r.pkg.Bin
			oldBinKeyToShim := make(map[string]string, len(prevBins))
			prevKeys := make([]string, 0, len(prevBins))
			for shimName, binKey := range prevBins {
				oldBinKeyToShim[binKey] = shimName
				prevKeys = append(prevKeys, binKey)
			}

			selected, discoverErr := asset.SelectBins(tr.bins, prevKeys, res.Name)
			switch {
			case errors.Is(discoverErr, asset.ErrSkip):
				pkgFailed = true
			case len(selected) == 0:
				printFail(cfg, "%s: no binary found", res.Name)
				hadErrors = true
				pkgFailed = true
			default:
				for _, s := range selected {
					print("%s: found bin [%s]", res.Name, s.Key())
				}
				base := proposedShimNames(tr.r.key, selected)
				rawKeys := make([]string, len(selected))
				proposed := make([]string, len(selected))
				for i, s := range selected {
					rawKeys[i] = s.Key()
					if oldShim, ok := oldBinKeyToShim[s.Key()]; ok {
						proposed[i] = oldShim
					} else {
						proposed[i] = base[i]
					}
				}
				pkgBase, _, _ := config.ParseVersionSuffix(tr.r.key)
				reserved := reservedShimNames(manifest, pkgBase)
				shimNames := proposed
				if hasReservedConflict(proposed, reserved) {
					renamed, promptErr := asset.PromptBinNames(rawKeys, proposed, reserved, res.Name)
					if errors.Is(promptErr, asset.ErrSkip) {
						pkgFailed = true
					} else if renamed != nil {
						shimNames = renamed
					}
				}
				if !pkgFailed {
					newBin = make(map[string]string, len(selected))
					for i, s := range selected {
						newBin[shimNames[i]] = s.Key()
					}
					for shimName := range prevBins {
						_ = shim.Remove(shimName)
					}
					for shimName, binKey := range newBin {
						binDir, binName := parseBinPath(binKey)
						if err := shim.Create(shimName, binName, tr.pkgDir, binDir); err != nil {
							printFail(cfg, "%s: %s: could not update shim: %v", res.Name, shimName, err)
							hadErrors = true
							pkgFailed = true
						}
					}
				}
			}
		}

		// Fonts likewise, preserving prior user-given names by path.
		if !pkgFailed && len(tr.fonts) > 0 {
			prevFonts := tr.r.pkg.Font
			oldPathToName := make(map[string]string, len(prevFonts))
			prevPaths := make([]string, 0, len(prevFonts))
			for fontName, fontPath := range prevFonts {
				oldPathToName[fontPath] = fontName
				prevPaths = append(prevPaths, fontPath)
			}

			selectedFonts, selErr := asset.SelectFonts(tr.fonts, prevPaths, res.Name)
			if selErr != nil && !errors.Is(selErr, asset.ErrSkip) {
				printFail(cfg, "%s: %v", res.Name, selErr)
				hadErrors = true
				pkgFailed = true
			} else if len(selectedFonts) > 0 {
				rawPaths := make([]string, len(selectedFonts))
				proposed := make([]string, len(selectedFonts))
				for i, sel := range selectedFonts {
					rawPaths[i] = sel.Key()
					if name, ok := oldPathToName[sel.Key()]; ok {
						proposed[i] = name
					} else {
						proposed[i] = asset.DeriveFontName(sel.FontName)
					}
				}
				pkgBase, _, _ := config.ParseVersionSuffix(tr.r.key)
				fontReserved := reservedFontNames(manifest, pkgBase)
				names := proposed
				if hasReservedConflict(proposed, fontReserved) {
					renamed, promptErr := asset.PromptFontConflicts(rawPaths, proposed, fontReserved, res.Name)
					if errors.Is(promptErr, asset.ErrSkip) {
						pkgFailed = true
					} else if renamed != nil {
						names = renamed
					}
				}
				if !pkgFailed {
					fontsDir, err := ensureFontDir()
					if err != nil {
						printFail(cfg, "%s: font dir: %v", res.Name, err)
						hadErrors = true
						pkgFailed = true
					} else {
						newFont = make(map[string]string, len(selectedFonts))
						for i, sel := range selectedFonts {
							srcPath := filepath.Join(tr.pkgDir, filepath.FromSlash(sel.Key()))
							if err := installFont(srcPath, fontsDir); err != nil {
								printFail(cfg, "%s: %s: could not install font: %v", res.Name, names[i], err)
								hadErrors = true
								pkgFailed = true
								continue
							}
							newFont[names[i]] = sel.Key()
						}
						newPaths := make([]string, 0, len(newFont))
						for _, fontPath := range newFont {
							newPaths = append(newPaths, fontPath)
						}
						for _, fontPath := range staleFontPaths(prevFonts, newPaths) {
							uninstallFont(fontPath, fontsDir)
						}
					}
				}
			}
		}

		if !pkgFailed && (len(tr.bins) > 0 || len(tr.fonts) > 0) {
			updated++
		}

		if !pkgFailed && tr.r.pkg.Version != newVer {
			if oldBase, err := dirs.ExtractBaseDir(tr.r.key); err == nil {
				if err := os.RemoveAll(filepath.Join(oldBase, tr.r.pkg.Version)); err != nil {
					printWarn(cfg, "%s: could not remove old extract dir: %v", res.Name, err)
				}
			}
		}

		if !pkgFailed && (len(newBin) > 0 || len(newFont) > 0) {
			manifest.Extracts[tr.r.key] = config.PackageEntry{
				Pin:     tr.r.pkg.Pin,
				Version: newVer,
				Assets:  assetNames(tr.r.chosens),
				Bin:     newBin,
				Font:    newFont,
			}
		}
	}

	if updated > 0 {
		printPass(cfg, "updated %d package(s)", updated)
	}

	if err := saveManifest(cfg, manifest); err != nil {
		return err
	}

	if hadErrors {
		return errSilent
	}
	return nil
}
