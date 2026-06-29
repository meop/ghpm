package cli

import (
	"errors"
	"fmt"
	"maps"
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
	// asset(s). Resolution is all-or-nothing: if every stored asset maps to a
	// unique, distinct asset in the new release the choices carry over silently;
	// the moment the mapping breaks down (an asset renamed, split, gone, or now
	// ambiguous) the prior selection can't be trusted, so we discard it and fall
	// back to add's fresh multi-select over the whole candidate list. A skipped
	// package drops out here and simply never reaches the final table.
	var ready []syncJob
	for _, o := range outdated {
		owner, repo, _ := gh.SplitSource(o.source)
		rel, err := ghClient.GetReleaseByTag(ctx, owner, repo, o.latestTag)
		if err != nil {
			printFail(cfg, "%s: %v", o.key, err)
			hadErrors = true
			continue
		}
		chosens, clean := resolvePriorAssets(rel.Assets, o.pkg.Assets)
		if !clean {
			pkgName, _, _ := config.ParseVersionSuffix(o.key)
			ac, acErr := asset.SelectAssetAuto(rel.Assets, cfg, "", pkgName)
			if acErr != nil {
				printFail(cfg, "%s: %v", o.key, acErr)
				hadErrors = true
				continue
			}
			picked, chErr := asset.PromptAssetsMulti(ac, o.key)
			if errors.Is(chErr, asset.ErrSkip) {
				continue
			}
			if chErr != nil {
				printFail(cfg, "%s: %v", o.key, chErr)
				hadErrors = true
				continue
			}
			chosens = picked
		}
		if len(chosens) == 0 {
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
		var newBinDeclined, newFontDeclined []string

		// Decide carry-vs-reprompt by comparing the *full* set of bins discovered
		// this release against the full set discovered at install time (selected +
		// declined, from the manifest). Identical → the package's layout is
		// unchanged, so the prior selection and shim names carry over silently
		// (nothing the user chose has changed; we only re-point shims at the new
		// version). Any difference → the layout changed, so the package is
		// reprompted from scratch via the same fresh flow add uses — including the
		// rename prompt. No prior shim name is ever reused silently once we reprompt.
		if len(tr.bins) > 0 {
			pkgBase, _, pinned := config.ParseVersionSuffix(tr.r.key)
			if prev := tr.r.pkg.DiscoveredBins(); len(prev) > 0 && sameStringSet(binKeys(tr.bins), prev) {
				newBin = maps.Clone(tr.r.pkg.Bin)
				newBinDeclined = slices.Clone(tr.r.pkg.BinDeclined)
				for _, binKey := range sortedValues(newBin) {
					print("%s: found bin [%s]", res.Name, binKey)
				}
			} else {
				reserved := reservedShimNames(manifest, pkgBase)
				bin, declined, skip, selErr := selectAndNameBins(tr.bins, tr.r.key, res.Name, pinned, reserved)
				switch {
				case selErr != nil:
					printFail(cfg, "%s: %v", res.Name, selErr)
					hadErrors = true
					pkgFailed = true
				case skip:
					pkgFailed = true
				default:
					newBin = bin
					newBinDeclined = declined
				}
			}
			if !pkgFailed && len(newBin) > 0 {
				for shimName := range tr.r.pkg.Bin {
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

		// Fonts follow the same carry-vs-reprompt rule against the full discovered
		// font set.
		if !pkgFailed && len(tr.fonts) > 0 {
			prevFonts := tr.r.pkg.Font
			pkgBase, _, _ := config.ParseVersionSuffix(tr.r.key)
			if prev := tr.r.pkg.DiscoveredFonts(); len(prev) > 0 && sameStringSet(fontKeys(tr.fonts), prev) {
				newFont = maps.Clone(prevFonts)
				newFontDeclined = slices.Clone(tr.r.pkg.FontDeclined)
				for _, fontName := range sortedKeys(newFont) {
					print("%s: found font [%s]", res.Name, fontName)
				}
			} else {
				fontReserved := reservedFontNames(manifest, pkgBase)
				font, declined, skip, selErr := selectAndNameFonts(tr.fonts, res.Name, fontReserved)
				switch {
				case selErr != nil:
					printFail(cfg, "%s: %v", res.Name, selErr)
					hadErrors = true
					pkgFailed = true
				case skip:
					pkgFailed = true
				default:
					newFont = font
					newFontDeclined = declined
				}
			}
			if !pkgFailed && len(newFont) > 0 {
				fontsDir, err := ensureFontDir()
				if err != nil {
					printFail(cfg, "%s: font dir: %v", res.Name, err)
					hadErrors = true
					pkgFailed = true
				} else {
					for fontName, fontPath := range newFont {
						srcPath := filepath.Join(tr.pkgDir, filepath.FromSlash(fontPath))
						if err := installFont(srcPath, fontsDir); err != nil {
							printFail(cfg, "%s: %s: could not install font: %v", res.Name, fontName, err)
							hadErrors = true
							pkgFailed = true
						}
					}
					for _, fontPath := range staleFontPaths(prevFonts, sortedValues(newFont)) {
						uninstallFont(fontPath, fontsDir)
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
				Pin:          tr.r.pkg.Pin,
				Version:      newVer,
				Assets:       assetNames(tr.r.chosens),
				Bin:          newBin,
				Font:         newFont,
				BinDeclined:  newBinDeclined,
				FontDeclined: newFontDeclined,
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

// resolvePriorAssets maps a package's previously selected assets onto the new
// release purely by hint (the stored asset name). It returns (chosens, true)
// only when every stored asset still resolves to a single, distinct asset,
// preserving the prior selection's count and identity. Anything else — an asset
// renamed, gone, now ambiguous, or two stored assets collapsing onto one — yields
// (nil, false) so the caller re-prompts the whole package from scratch rather
// than silently carrying over a half-matched (or scoring-guessed) set.
func resolvePriorAssets(assets []gh.Asset, oldNames []string) ([]gh.Asset, bool) {
	if len(oldNames) == 0 {
		return nil, false
	}
	chosens := make([]gh.Asset, 0, len(oldNames))
	seen := make(map[string]bool, len(oldNames))
	for _, name := range oldNames {
		match, ok := asset.ResolveByHint(assets, name)
		if !ok || seen[match.Name] {
			return nil, false
		}
		seen[match.Name] = true
		chosens = append(chosens, match)
	}
	return chosens, true
}
