package cli

import (
	"errors"
	"fmt"
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

func newAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "add <name> [name...]",
		Aliases: []string{"ad", "in", "install"},
		Short:   "Add packages from releases",
		Args:    cobra.MinimumNArgs(1),
		RunE:    runAdd,
	}
	cmd.Flags().BoolP("force", "f", false, "Reinstall even if already installed")
	addSkipHashCheckFlag(cmd)
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
	chosens []gh.Asset
}

type installTaskResult struct {
	r jobWithRelease
	extractResult
}

func runAdd(cmd *cobra.Command, args []string) error {
	forceInstall, _ := cmd.Flags().GetBool("force")
	ci, err := initCommand(cmdOptions{Lock: true, Manifest: true, GH: true, Dirs: true, Repos: true, SkipHashCheck: true})
	if err != nil {
		return err
	}
	defer ci.close()
	cfg := ci.cfg
	manifest := ci.manifest
	repos := ci.repos
	ghClient := ci.gh
	dirs := ci.dirs
	ctx := cmd.Context()

	var ready []jobWithRelease
	var hadErrors bool

	for _, arg := range args {
		pkgName, ver, pinned := config.ParseVersionSuffix(arg)

		var explicitSource string
		if src, repoName, err := parseSourceArg(pkgName); err != nil {
			printFail(cfg, "%s: %v", pkgName, err)
			hadErrors = true
			continue
		} else if src != "" {
			explicitSource = src
			pkgName = repoName
		}

		if pkgName == binGh || pkgName == binGhpm {
			print("%s: already self managed", pkgName)
			continue
		}
		if explicitSource == "" {
			if err := config.ValidateName(pkgName); err != nil {
				printFail(cfg, "%s: %v", pkgName, err)
				hadErrors = true
				continue
			}
		}

		var source string
		if explicitSource != "" {
			source = explicitSource
			print("%s: repo → %s", pkgName, source)
		} else {
			var found bool
			source, found = config.LookupSource(pkgName, manifest, repos)
			if !found {
				var err error
				source, err = config.SearchGitHub(pkgName)
				if err != nil {
					printFail(cfg, "%s: %v", pkgName, err)
					hadErrors = true
					continue
				}
			}
			print("%s: repo → %s", pkgName, source)
		}

		partialJob := installJob{name: pkgName, version: ver, pinned: pinned}
		if entry, exists := manifest.Extracts[partialJob.key()]; exists && !forceInstall {
			print("%s: already added → %s", pkgName, entry.Version)
			continue
		}
		if !pinned {
			if existing, found := manifest.FindBySource(source); found && existing != pkgName && !forceInstall {
				print("%s: already added as %s", pkgName, existing)
				continue
			}
		}

		owner, repo, err := gh.SplitSource(source)
		if err != nil {
			printFail(cfg, "%s: %v", pkgName, err)
			hadErrors = true
			continue
		}
		var rel gh.Release
		if !pinned {
			rel, err = ghClient.GetLatestRelease(ctx, owner, repo)
		} else {
			c, perr := config.ParseConstraint(ver)
			if perr != nil {
				printFail(cfg, "%s: %v", pkgName, perr)
				hadErrors = true
				continue
			}
			if c.Level == config.PinExact {
				rel, err = ghClient.GetReleaseByTag(ctx, owner, repo, ver)
			} else {
				rel, err = ghClient.FindLatestMatching(ctx, owner, repo, c)
			}
		}
		if err != nil {
			printFail(cfg, "%s: %v", pkgName, err)
			hadErrors = true
			continue
		}

		ac, err := asset.SelectAssetAuto(rel.Assets, cfg, "", pkgName)
		if err != nil {
			printFail(cfg, "%s: %v", pkgName, err)
			hadErrors = true
			continue
		}
		if ac.Chosen.Name == "" {
			sep()
		}
		chosens, err := asset.PromptAssetsMulti(ac)
		if errors.Is(err, asset.ErrSkip) {
			continue
		}
		if err != nil {
			printFail(cfg, "%s: %v", pkgName, err)
			hadErrors = true
			continue
		}
		if ac.Chosen.Name != "" {
			print("%s: asset → %s", pkgName, chosens[0].Name)
		}
		ready = append(ready, jobWithRelease{
			job:     installJob{name: pkgName, source: source, version: ver, pinned: pinned},
			release: rel,
			chosens: chosens,
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
			for _, c := range r.chosens {
				print("%s: install %s (asset: %s)", r.job.name, config.NormalizeVersion(r.release.TagName), c.Name)
			}
		}
		return nil
	}

	if !promptInstall(cfg, ready) {
		return nil
	}
	hasOutput = false

	installTasks := make([]parallel.Task[installTaskResult], len(ready))
	for i, r := range ready {
		installTasks[i] = parallel.Task[installTaskResult]{
			Name: r.job.name,
			Run: func() (installTaskResult, error) {
				owner, repo, _ := gh.SplitSource(r.job.source)
				cacheDir, err := dirs.ReleaseDir(r.job.source, r.release.TagName)
				if err != nil {
					return installTaskResult{}, err
				}
				ver := config.NormalizeVersion(r.release.TagName)
				ex, err := downloadAndExtract(ctx, cfg, ghClient, dirs, owner, repo, r.release.TagName, cacheDir, r.job.name, r.job.key(), ver, r.job.name, r.chosens)
				if err != nil {
					return installTaskResult{}, err
				}
				return installTaskResult{r: r, extractResult: ex}, nil
			},
		}
	}

	installResults := parallel.Run(cmd.Context(), installTasks, cfg.NumParallel)

	type shimPlan struct {
		key           string
		jobName       string
		source        string
		pkgDirByAsset map[string]string
		binsByAsset   map[string]map[string]string
		pin           string
		version       string
		fontAssets    map[string]map[string]string
	}
	var shimPlans []shimPlan

	for _, res := range installResults {
		if res.Err != nil {
			printFail(cfg, "%s: %v", res.Name, res.Err)
			hadErrors = true
			continue
		}
		tr := res.Value
		r := tr.r

		if len(tr.binsByAsset) == 0 && len(tr.fontsByAsset) == 0 {
			for _, c := range r.chosens {
				printFail(cfg, "%s: no binaries or fonts found in %s", r.job.name, c.Name)
			}
			hadErrors = true
			continue
		}

		var binsByAsset map[string]map[string]string
		if len(tr.binsByAsset) > 0 {
			key := r.job.key()
			_, _, pinned := config.ParseVersionSuffix(key)

			binAssetNames := make([]string, 0, len(tr.binsByAsset))
			for a := range tr.binsByAsset {
				binAssetNames = append(binAssetNames, a)
			}
			slices.Sort(binAssetNames)

			type binPos struct{ asset, binKey string }
			var positions []binPos
			var rawKeys, proposed []string
			needRename := false
			selectionFailed := false
			for _, assetName := range binAssetNames {
				selected, discoverErr := asset.SelectBins(tr.binsByAsset[assetName], nil)
				if errors.Is(discoverErr, asset.ErrSkip) {
					selectionFailed = true
					break
				}
				if len(selected) == 0 {
					printFail(cfg, "%s: no binary found in %s", r.job.name, assetName)
					hadErrors = true
					selectionFailed = true
					break
				}
				if !pinned && needsShimRenamePrompt(r.job.name, selected) {
					needRename = true
				}
				names := proposedShimNames(key, selected)
				for i, s := range selected {
					printInfo(cfg, "%s: bin %s", r.job.name, s.Key())
					positions = append(positions, binPos{assetName, s.Key()})
					rawKeys = append(rawKeys, s.Key())
					proposed = append(proposed, names[i])
				}
			}
			if selectionFailed {
				continue
			}

			reserved := reservedShimNames(manifest, r.job.name)
			for _, prev := range shimPlans {
				if prev.jobName == r.job.name {
					continue
				}
				for _, prevBins := range prev.binsByAsset {
					for shimName := range prevBins {
						reserved[shimName] = prev.jobName
					}
				}
			}
			shimNames := proposed
			if hasReservedConflict(proposed, reserved) || (!pinned && needRename) {
				sep()
				renamed, promptErr := asset.PromptBinNames(rawKeys, proposed, reserved)
				if errors.Is(promptErr, asset.ErrSkip) {
					continue
				}
				if renamed != nil {
					shimNames = renamed
				}
			}
			binsByAsset = make(map[string]map[string]string)
			for i, pos := range positions {
				if binsByAsset[pos.asset] == nil {
					binsByAsset[pos.asset] = make(map[string]string)
				}
				binsByAsset[pos.asset][shimNames[i]] = pos.binKey
			}
		}

		var fontAssets map[string]map[string]string
		if len(tr.fontsByAsset) > 0 {
			fontAssets = make(map[string]map[string]string)
			fontReserved := make(map[string]string)
			for mKey, mEntry := range manifest.Extracts {
				ownerPkg, _, _ := config.ParseVersionSuffix(mKey)
				if ownerPkg == r.job.name {
					continue
				}
				for fontName := range mEntry.AllFonts() {
					fontReserved[fontName] = ownerPkg
				}
			}
			for _, prev := range shimPlans {
				if prev.jobName == r.job.name {
					continue
				}
				for _, fonts := range prev.fontAssets {
					for fontName := range fonts {
						fontReserved[fontName] = prev.jobName
					}
				}
			}
			assetNames := make([]string, 0, len(tr.fontsByAsset))
			for a := range tr.fontsByAsset {
				assetNames = append(assetNames, a)
			}
			slices.Sort(assetNames)
			for i, assetName := range assetNames {
				if i > 0 {
					sep()
				}
				candidates := tr.fontsByAsset[assetName]
				selectedFonts, selErr := asset.SelectFonts(candidates, nil)
				if errors.Is(selErr, asset.ErrSkip) {
					continue
				}
				if selErr != nil || len(selectedFonts) == 0 {
					continue
				}
				namedFonts, promptErr := asset.PromptFontNames(selectedFonts, fontReserved)
				if errors.Is(promptErr, asset.ErrSkip) {
					continue
				}
				if len(namedFonts) > 0 {
					fontAssets[assetName] = namedFonts
					for fontName := range namedFonts {
						fontReserved[fontName] = r.job.name
					}
					fontNames := make([]string, 0, len(namedFonts))
					for k := range namedFonts {
						fontNames = append(fontNames, k)
					}
					slices.Sort(fontNames)
					for _, fontName := range fontNames {
						printInfo(cfg, "%s: font %s", r.job.name, fontName)
					}
				}
			}
		}

		if len(binsByAsset) == 0 && len(fontAssets) == 0 {
			continue
		}

		shimPlans = append(shimPlans, shimPlan{
			key:           r.job.key(),
			jobName:       r.job.name,
			source:        r.job.source,
			pkgDirByAsset: tr.pkgDirByAsset,
			binsByAsset:   binsByAsset,
			pin:           r.job.pin(),
			version:       config.NormalizeVersion(r.release.TagName),
			fontAssets:    fontAssets,
		})
	}

	if len(shimPlans) == 0 {
		if hadErrors {
			return errSilent
		}
		return nil
	}

	type installRow struct{ pkg, version, artifact, target string }
	var tableRows []installRow
	totalFonts := 0
	for _, p := range shimPlans {
		for _, bins := range p.binsByAsset {
			for shimName, binKey := range bins {
				tableRows = append(tableRows, installRow{p.key, p.version, binKey, shimName})
			}
		}
		for _, fonts := range p.fontAssets {
			for fontName, fontPath := range fonts {
				tableRows = append(tableRows, installRow{p.key, p.version, fontPath, fontName})
				totalFonts++
			}
		}
	}
	if len(tableRows) > 0 {
		slices.SortFunc(tableRows, func(a, b installRow) int {
			if c := strings.Compare(a.pkg, b.pkg); c != 0 {
				return c
			}
			return strings.Compare(a.target, b.target)
		})
		rows := make([][]string, len(tableRows))
		for i, r := range tableRows {
			rows[i] = []string{r.pkg, r.version, r.artifact, r.target}
		}
		printTable([]string{"name", "version", "artifact", "target"}, rows, nil)
	}
	totalBins := len(tableRows) - totalFonts
	var confirmMsg string
	switch {
	case totalBins > 0 && totalFonts > 0:
		confirmMsg = fmt.Sprintf("create %d bin(s) and %d font(s)", totalBins, totalFonts)
	case totalBins > 0:
		confirmMsg = fmt.Sprintf("create %d bin(s)", totalBins)
	default:
		confirmMsg = fmt.Sprintf("install %d font(s)", totalFonts)
	}
	if !promptConfirm(confirmMsg) {
		if hadErrors {
			return errSilent
		}
		return nil
	}
	hasOutput = false

	successCount := 0
	for _, p := range shimPlans {
		if forceInstall {
			if existing, ok := manifest.Extracts[p.key]; ok {
				for shimName := range existing.AllBins() {
					_ = shim.Remove(shimName)
				}
				if oldFonts := existing.AllFonts(); len(oldFonts) > 0 {
					var newPaths []string
					for _, fonts := range p.fontAssets {
						for _, fontPath := range fonts {
							newPaths = append(newPaths, fontPath)
						}
					}
					if fontsDir, err := userFontDir(); err == nil {
						for _, fontPath := range staleFontPaths(oldFonts, newPaths) {
							uninstallFont(fontPath, fontsDir)
						}
					}
				}
			}
		}
		manifest.Repos[p.jobName] = p.source
		assets := make(map[string]config.AssetEntry)
		for assetName, bins := range p.binsByAsset {
			ae := assets[assetName]
			ae.Bin = bins
			assets[assetName] = ae
		}
		for assetName, fonts := range p.fontAssets {
			ae := assets[assetName]
			ae.Font = fonts
			assets[assetName] = ae
		}
		manifest.Extracts[p.key] = config.PackageEntry{
			Pin:     p.pin,
			Version: p.version,
			Asset:   assets,
		}
		shimFailed := false
		for assetName, bins := range p.binsByAsset {
			for shimName, binsKey := range bins {
				binDir, binName := parseBinPath(binsKey)
				if err := shim.Create(shimName, binName, p.pkgDirByAsset[assetName], binDir); err != nil {
					printFail(cfg, "%s: %s: could not create shim: %v", p.jobName, shimName, err)
					shimFailed = true
					hadErrors = true
				}
			}
		}
		fontFailed := false
		if len(p.fontAssets) > 0 {
			fontsDir, err := ensureFontDir()
			if err != nil {
				printFail(cfg, "%s: font dir: %v", p.jobName, err)
				fontFailed = true
				hadErrors = true
			}
			if !fontFailed {
				for assetName, fonts := range p.fontAssets {
					fontNames := make([]string, 0, len(fonts))
					for fontName := range fonts {
						fontNames = append(fontNames, fontName)
					}
					slices.Sort(fontNames)
					for _, fontName := range fontNames {
						fontPath := fonts[fontName]
						srcPath := filepath.Join(p.pkgDirByAsset[assetName], filepath.FromSlash(fontPath))
						if err := installFont(srcPath, fontsDir); err != nil {
							printFail(cfg, "%s: %s: could not install font: %v", p.jobName, fontName, err)
							hadErrors = true
						}
					}
				}
			}
		}
		if !shimFailed && !fontFailed {
			successCount++
		}
	}
	if successCount > 0 {
		printPass(cfg, "installed %d package(s)", successCount)
	}

	if err := saveManifest(cfg, manifest); err != nil {
		return err
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
		src = "github.com/" + src
	}
	_, repo, splitErr := gh.SplitSource(src)
	if splitErr != nil {
		return "", "", fmt.Errorf("invalid source %q: must be org/repo or host/org/repo", name)
	}
	return src, repo, nil
}

func promptInstall(cfg *config.Settings, ready []jobWithRelease) bool {
	var rows [][]string
	for _, r := range ready {
		for _, c := range r.chosens {
			rows = append(rows, []string{r.job.key(), config.NormalizeVersion(r.release.TagName), r.job.pin(), r.job.source, c.Name})
		}
	}
	colors := []func(string) string{nil, colorfn(cfg, "new"), nil, nil, nil}
	printTable([]string{"name", "version", "pin", "repo", "asset"}, rows, colors)
	return promptConfirm(fmt.Sprintf("install %d package(s)", len(ready)))
}
