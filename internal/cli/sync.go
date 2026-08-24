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
		Aliases: []string{"s", "sy", "up", "update"},
		Short:   "Sync packages to their latest releases",
		RunE:    runSync,
	}
	addSkipHashCheckFlag(cmd)
	cmd.Flags().BoolP("force", "f", false, "Reinstall even if already at latest version")
	return cmd
}

func runSync(cmd *cobra.Command, args []string) error {
	forceSync, _ := cmd.Flags().GetBool("force")
	ctx := cmd.Context()
	ci, err := initCommand(ctx, cmdOptions{Lock: true, Manifest: true, GH: true, Shim: true, SkipHashCheck: true})
	if err != nil {
		return err
	}
	defer ci.close()
	cfg := ci.cfg
	manifest := ci.manifest
	ghClient := ci.gh
	dirs := ci.dirs

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
	var failedItems []failedItem

	for _, res := range batchResults {
		if res.Err != nil {
			if gh.IsRateLimited(res.Err) {
				skipped++
				printRateLimited(cfg, res.Key)
				continue
			}
			printFail(cfg, "%s: %v", res.Key, res.Err)
			hadErrors = true
			failedItems = append(failedItems, failedItem{name: res.Key, reason: res.Err.Error()})
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
			printFailedTable(cfg, "package", failedItems)
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
			failedItems = append(failedItems, failedItem{name: o.key, reason: err.Error()})
			continue
		}
		chosens, clean := resolvePriorAssets(rel.Assets, o.pkg.Assets, config.NormalizeVersion(o.latestTag))
		if !clean {
			pkgName, _, _ := config.ParseVersionSuffix(o.key)
			ac, acErr := asset.SelectAssetAuto(rel.Assets, cfg, "", pkgName)
			if acErr != nil {
				printFail(cfg, "%s: %v", o.key, acErr)
				hadErrors = true
				failedItems = append(failedItems, failedItem{name: o.key, reason: acErr.Error()})
				continue
			}
			picked, chErr := asset.PromptAssetsMulti(ac, o.key)
			if errors.Is(chErr, asset.ErrSkip) {
				continue
			}
			if chErr != nil {
				printFail(cfg, "%s: %v", o.key, chErr)
				hadErrors = true
				failedItems = append(failedItems, failedItem{name: o.key, reason: chErr.Error()})
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
			printFailedTable(cfg, "package", failedItems)
			return errSilent
		}
		return nil
	}

	// Flatten every package's chosen assets into one list so all of them compete
	// for a single numParallel-wide pool, rather than nesting a pool per package
	// inside the per-package pool below.
	var downloads []assetDownload
	cacheDirs := make([]string, len(ready))
	for i, r := range ready {
		owner, repo, _ := gh.SplitSource(r.source)
		cacheDir, err := dirs.ReleaseDir(r.source, r.release.TagName)
		if err != nil {
			printFail(cfg, "%s: %v", r.key, err)
			hadErrors = true
			failedItems = append(failedItems, failedItem{name: r.key, reason: err.Error()})
			continue
		}
		cacheDirs[i] = cacheDir
		for _, a := range r.chosens {
			downloads = append(downloads, assetDownload{
				pkgIdx: i, owner: owner, repo: repo, tagName: r.release.TagName,
				cacheDir: cacheDir, displayName: r.key, asset: a,
			})
		}
	}
	downloadErrs := downloadAllAssets(ctx, ghClient, downloads, cfg.NumParallel)

	syncTasks := make([]parallel.Task[syncTaskResult], 0, len(ready))
	for i, r := range ready {
		if cacheDirs[i] == "" {
			continue
		}
		if err, failed := downloadErrs[i]; failed {
			printFail(cfg, "%s: %v", r.key, err)
			hadErrors = true
			failedItems = append(failedItems, failedItem{name: r.key, reason: err.Error()})
			continue
		}
		syncTasks = append(syncTasks, parallel.Task[syncTaskResult]{
			Name: r.key,
			Run: func() (syncTaskResult, error) {
				newVersion := config.NormalizeVersion(r.release.TagName)
				ex, err := extractOverlay(dirs, r.key, newVersion, cacheDirs[i], r.key, r.chosens)
				if err != nil {
					return syncTaskResult{}, err
				}
				return syncTaskResult{r: r, extractResult: ex}, nil
			},
		})
	}

	updated := 0
	for _, res := range parallel.Run(cmd.Context(), syncTasks, cfg.NumParallel) {
		if res.Err != nil {
			printFail(cfg, "%s: %v", res.Name, res.Err)
			hadErrors = true
			failedItems = append(failedItems, failedItem{name: res.Name, reason: res.Err.Error()})
			continue
		}
		tr := res.Value
		newVer := config.NormalizeVersion(tr.r.release.TagName)

		if len(tr.bins) == 0 && len(tr.fonts) == 0 {
			reason := fmt.Sprintf("no binaries or fonts found in %s", strings.Join(assetNames(tr.r.chosens), ", "))
			printFail(cfg, "%s: %s", res.Name, reason)
			hadErrors = true
			failedItems = append(failedItems, failedItem{name: res.Name, reason: reason})
			continue
		}

		pkgFailed := false
		var failReason string
		var newBin, newFont map[string]string
		var newBinDeclined, newFontDeclined []string

		// Decide carry-vs-reprompt by comparing the *full* set of bins discovered
		// this release against the full set discovered at install time (selected +
		// declined, from the manifest), tolerating a casing-only rename
		// (sameKeySetFold) the same way fonts do. Identical (aside from case) → the
		// package's layout is unchanged, so the prior selection and shim names
		// carry over silently (nothing the user chose has changed; we only
		// re-point shims at the new version, recasing the stored path when the
		// release re-cased a file). Any other difference → the layout changed, so
		// the package is reprompted from scratch via the same fresh flow add uses
		// — including the rename prompt. No prior shim name is ever reused
		// silently once we reprompt.
		if len(tr.bins) > 0 {
			pkgBase, _, pinned := config.ParseVersionSuffix(tr.r.key)
			prev := tr.r.pkg.DiscoveredBins()
			recase, foldOK := sameKeySetFold(binKeys(tr.bins), prev)
			if len(prev) > 0 && foldOK {
				newBin = make(map[string]string, len(tr.r.pkg.Bin))
				for shimName, oldKey := range tr.r.pkg.Bin {
					newBin[shimName] = recase[oldKey]
				}
				newBinDeclined = make([]string, len(tr.r.pkg.BinDeclined))
				for i, oldKey := range tr.r.pkg.BinDeclined {
					newBinDeclined[i] = recase[oldKey]
				}
				for _, binKey := range sortedValues(newBin) {
					print("%s: bin found [%s]", res.Name, binKey)
				}
			} else {
				reserved := reservedShimNames(manifest, pkgBase)
				bin, declined, skip, selErr := selectAndNameBins(tr.bins, tr.r.key, res.Name, pinned, reserved)
				switch {
				case selErr != nil:
					printFail(cfg, "%s: %v", res.Name, selErr)
					hadErrors = true
					pkgFailed = true
					failReason = selErr.Error()
				case skip:
					pkgFailed = true
				default:
					newBin = bin
					newBinDeclined = declined
				}
			}
		}
		// Sync shims to newBin whenever the package had or gains any bins — not
		// just when len(tr.bins) > 0 above, since a release can drop every bin a
		// package used to have (newBin then stays empty) and the old shims still
		// need removing.
		if !pkgFailed && (len(tr.r.pkg.Bin) > 0 || len(newBin) > 0) {
			for _, e := range syncBinShims(tr.pkgDir, tr.r.pkg.Bin, newBin) {
				printFail(cfg, "%s: %s: could not update shim: %v", res.Name, e.name, e.err)
				hadErrors = true
				pkgFailed = true
				if failReason == "" {
					failReason = fmt.Sprintf("%s: could not update shim: %v", e.name, e.err)
				}
			}
		}

		// Fonts follow the same carry-vs-reprompt rule against the full discovered
		// font set, including bin's tolerance for a casing-only rename
		// (sameKeySetFold) — no reprompt is warranted just because a release
		// re-cased a file.
		if !pkgFailed && len(tr.fonts) > 0 {
			pkgBase, _, _ := config.ParseVersionSuffix(tr.r.key)
			prev := tr.r.pkg.DiscoveredFonts()
			recase, foldOK := sameKeySetFold(fontKeys(tr.fonts), prev)
			if len(prev) > 0 && foldOK {
				newFont = make(map[string]string, len(tr.r.pkg.Font))
				for fontName, oldPath := range tr.r.pkg.Font {
					newFont[fontName] = recase[oldPath]
				}
				newFontDeclined = make([]string, len(tr.r.pkg.FontDeclined))
				for i, oldPath := range tr.r.pkg.FontDeclined {
					newFontDeclined[i] = recase[oldPath]
				}
				for _, fontName := range sortedKeys(newFont) {
					print("%s: font found [%s]", res.Name, fontName)
				}
			} else {
				fontReserved := reservedFontNames(manifest, pkgBase)
				font, declined, skip, selErr := selectAndNameFonts(tr.fonts, res.Name, fontReserved)
				switch {
				case selErr != nil:
					printFail(cfg, "%s: %v", res.Name, selErr)
					hadErrors = true
					pkgFailed = true
					if failReason == "" {
						failReason = selErr.Error()
					}
				case skip:
					pkgFailed = true
				default:
					newFont = font
					newFontDeclined = declined
				}
			}
		}
		// Sync installed fonts to newFont whenever the package had or gains any
		// fonts — mirrors the bin handling above: a release can drop every font a
		// package used to have, and the old ones still need uninstalling.
		if !pkgFailed && (len(tr.r.pkg.Font) > 0 || len(newFont) > 0) {
			fontErrs, err := syncPkgFonts(tr.pkgDir, tr.r.pkg.Font, newFont)
			if err != nil {
				printFail(cfg, "%s: font dir: %v", res.Name, err)
				hadErrors = true
				pkgFailed = true
				if failReason == "" {
					failReason = fmt.Sprintf("font dir: %v", err)
				}
			}
			for _, e := range fontErrs {
				printFail(cfg, "%s: %s: could not install font: %v", res.Name, e.name, e.err)
				hadErrors = true
				pkgFailed = true
				if failReason == "" {
					failReason = fmt.Sprintf("%s: could not install font: %v", e.name, e.err)
				}
			}
		}

		if !pkgFailed && (len(tr.bins) > 0 || len(tr.fonts) > 0) {
			updated++
		} else if failReason != "" {
			failedItems = append(failedItems, failedItem{name: res.Name, reason: failReason})
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
	printFailedTable(cfg, "package", failedItems)

	if err := saveManifest(cfg, manifest); err != nil {
		return err
	}

	if hadErrors {
		return errSilent
	}
	return nil
}

// shimSyncErr is one shim-create failure from syncBinShims, naming the shim
// that failed.
type shimSyncErr struct {
	name string
	err  error
}

// syncBinShims removes every shim named in oldBin, then creates a shim for
// every entry in newBin. Both maps may be empty — ranging over an empty map
// is a no-op — so a package whose bins vanish entirely (newBin empty) still
// gets its stale shims removed.
func syncBinShims(pkgDir string, oldBin, newBin map[string]string) []shimSyncErr {
	for shimName := range oldBin {
		_ = shim.Remove(shimName)
	}
	var errs []shimSyncErr
	for shimName, binKey := range newBin {
		binDir, binName := parseBinPath(binKey)
		if err := shim.Create(shimName, binName, pkgDir, binDir, false); err != nil {
			errs = append(errs, shimSyncErr{shimName, err})
		}
	}
	return errs
}

// fontSyncErr is one font-install failure from syncPkgFonts, naming the font
// that failed.
type fontSyncErr struct {
	name string
	err  error
}

// syncPkgFonts installs every entry in newFont, then removes any previously
// installed font whose file is no longer among newFont's paths. If both maps
// are empty there's nothing to do, so fontsDir isn't even created.
func syncPkgFonts(pkgDir string, oldFont, newFont map[string]string) ([]fontSyncErr, error) {
	if len(oldFont) == 0 && len(newFont) == 0 {
		return nil, nil
	}
	fontsDir, err := ensureFontDir()
	if err != nil {
		return nil, err
	}
	var errs []fontSyncErr
	for fontName, fontPath := range newFont {
		srcPath := filepath.Join(pkgDir, filepath.FromSlash(fontPath))
		if err := installFont(srcPath, fontsDir); err != nil {
			errs = append(errs, fontSyncErr{fontName, err})
		}
	}
	for _, fontPath := range staleFontPaths(oldFont, sortedValues(newFont)) {
		uninstallFont(fontPath, fontsDir)
	}
	return errs, nil
}

// resolvePriorAssets maps a package's previously selected assets onto the new
// release purely by hint (the stored asset name), allowing at most a version
// bump in the differing token — and only when that bump lands on newVersion,
// the release actually being resolved (see asset.ResolveByHint). It returns
// (chosens, true) only when every stored asset still resolves to a single,
// distinct asset, preserving the prior selection's count and identity.
// Anything else — an asset renamed, gone, now ambiguous, bumped to some other
// version, or two stored assets collapsing onto one — yields (nil, false) so
// the caller re-prompts the whole package from scratch rather than silently
// carrying over a half-matched (or scoring-guessed) set.
func resolvePriorAssets(assets []gh.Asset, oldNames []string, newVersion string) ([]gh.Asset, bool) {
	if len(oldNames) == 0 {
		return nil, false
	}
	chosens := make([]gh.Asset, 0, len(oldNames))
	seen := make(map[string]bool, len(oldNames))
	for _, name := range oldNames {
		match, ok := asset.ResolveByHint(assets, name, newVersion)
		if !ok || seen[match.Name] {
			return nil, false
		}
		seen[match.Name] = true
		chosens = append(chosens, match)
	}
	return chosens, true
}
