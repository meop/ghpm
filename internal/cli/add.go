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
	chosens []gh.Asset
}

type installTaskResult struct {
	r            jobWithRelease
	pkgDir       string
	fontsByAsset map[string][]asset.FontCandidate
	binsByAsset  map[string][]asset.BinaryCandidate
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
		chosens, err := asset.PromptAssetsMulti(ac)
		if errors.Is(err, asset.ErrSkip) {
			continue
		}
		if err != nil {
			printFail(cfg, "%v", err)
			hadErrors = true
			continue
		}
		if ac.Chosen.Name != "" {
			printInfo(cfg, "asset: %s", chosens[0].Name)
		}
		ready = append(ready, jobWithRelease{
			job:     installJob{name: name, source: source, version: ver, pinned: pinned},
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
			names := make([]string, len(r.chosens))
			for i, c := range r.chosens {
				names[i] = c.Name
			}
			fmt.Printf("%s: install %s (asset: %s)\n", r.job.name, config.NormalizeVersion(r.release.TagName), strings.Join(names, ", "))
		}
		return nil
	}

	if !promptInstall(cfg, ready) {
		return nil
	}
	hasOutput = false

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
				version := config.NormalizeVersion(r.release.TagName)
				pkgDir, err := store.ExtractDir(r.job.key(), version)
				if err != nil {
					return nil, err
				}
				if err := os.RemoveAll(pkgDir); err != nil {
					return nil, err
				}
				if err := os.MkdirAll(pkgDir, 0755); err != nil {
					return nil, err
				}

				fontsByAsset := make(map[string][]asset.FontCandidate)
				binsByAsset := make(map[string][]asset.BinaryCandidate)

				for _, chosen := range r.chosens {
					if _, err := os.Stat(filepath.Join(cacheDir, chosen.Name)); os.IsNotExist(err) {
						printInfo(cfg, "%s: downloading %s...", r.job.name, chosen.Name)
						if err := gh.DownloadAsset(ctx, owner, repo, r.release.TagName, chosen.Name, cacheDir); err != nil {
							return nil, err
						}
					}
					if !noVerify {
						if _, err := gh.VerifyAsset(ctx, owner, repo, r.release.TagName, cacheDir, chosen.Name); err != nil {
							return nil, err
						}
					}

					prevFonts := fontKeySet(asset.FindFonts(pkgDir))
					prevBins := binKeySet(asset.FindBinaries(pkgDir, r.job.name))

					if err := asset.ExtractPackage(cacheDir, chosen.Name, pkgDir); err != nil {
						_ = os.RemoveAll(pkgDir)
						return nil, err
					}

					if newFonts := filterNewFonts(asset.FindFonts(pkgDir), prevFonts); len(newFonts) > 0 {
						fontsByAsset[chosen.Name] = newFonts
					}
					if newBins := filterNewBins(asset.FindBinaries(pkgDir, r.job.name), prevBins); len(newBins) > 0 {
						binsByAsset[chosen.Name] = newBins
					}
				}

				return installTaskResult{r: r, pkgDir: pkgDir, fontsByAsset: fontsByAsset, binsByAsset: binsByAsset}, nil
			},
		}
	}

	installResults := parallel.Run(cmd.Context(), installTasks, cfg.NumParallel)

	type shimPlan struct {
		key        string
		jobName    string
		source     string
		pkgDir     string
		bins       map[string]string
		pin        string
		version    string
		binAsset   string
		fontAssets map[string]map[string]string
	}
	var shimPlans []shimPlan

	for _, res := range installResults {
		printTitle(res.Name)
		if res.Err != nil {
			printFail(cfg, "%v", res.Err)
			hadErrors = true
			continue
		}
		tr, ok := res.Value.(installTaskResult)
		if !ok {
			continue
		}
		r := tr.r

		if len(tr.binsByAsset) == 0 && len(tr.fontsByAsset) == 0 {
			assetNames := make([]string, len(r.chosens))
			for i, c := range r.chosens {
				assetNames[i] = c.Name
			}
			printFail(cfg, "no binaries or fonts found in %s", strings.Join(assetNames, ", "))
			hadErrors = true
			continue
		}

		var bins map[string]string
		var binAsset string
		if len(tr.binsByAsset) > 0 {
			var allBinCandidates []asset.BinaryCandidate
			for assetName, candidates := range tr.binsByAsset {
				if binAsset == "" {
					binAsset = assetName
				}
				allBinCandidates = append(allBinCandidates, candidates...)
			}
			selected, discoverErr := asset.SelectBinaries(allBinCandidates, nil)
			if errors.Is(discoverErr, asset.ErrSkip) {
				continue
			}
			if len(selected) == 0 {
				printFail(cfg, "no binary found in %s", binAsset)
				hadErrors = true
				continue
			}
			key := r.job.key()
			rawKeys := make([]string, len(selected))
			for i, s := range selected {
				rawKeys[i] = s.Key()
			}
			printInfo(cfg, "bin %s", strings.Join(rawKeys, ", "))
			_, _, pinned := config.ParseVersionSuffix(key)
			proposed := proposedShimNames(key, selected)
			reserved := make(map[string]string)
			for mKey, entry := range manifest.Extracts {
				ownerPkg, _, _ := config.ParseVersionSuffix(mKey)
				if ownerPkg == r.job.name {
					continue
				}
				for shimName := range entry.AllBins() {
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
			bins = make(map[string]string, len(shimNames))
			for i, s := range selected {
				bins[shimNames[i]] = s.Key()
			}
		}

		var fontAssets map[string]map[string]string
		if len(tr.fontsByAsset) > 0 {
			fontAssets = make(map[string]map[string]string)
			assetNames := make([]string, 0, len(tr.fontsByAsset))
			for a := range tr.fontsByAsset {
				assetNames = append(assetNames, a)
			}
			slices.Sort(assetNames)
			for _, assetName := range assetNames {
				candidates := tr.fontsByAsset[assetName]
				selectedFonts, selErr := asset.SelectFonts(candidates, nil)
				if errors.Is(selErr, asset.ErrSkip) {
					continue
				}
				if selErr != nil || len(selectedFonts) == 0 {
					continue
				}
				namedFonts := asset.PromptFontNames(selectedFonts)
				if len(namedFonts) > 0 {
					fontAssets[assetName] = namedFonts
					fontNames := make([]string, 0, len(namedFonts))
					for k := range namedFonts {
						fontNames = append(fontNames, k)
					}
					slices.Sort(fontNames)
					printInfo(cfg, "fonts: %s", strings.Join(fontNames, ", "))
				}
			}
		}

		if len(bins) == 0 && len(fontAssets) == 0 {
			continue
		}

		shimPlans = append(shimPlans, shimPlan{
			key:        r.job.key(),
			jobName:    r.job.name,
			source:     r.job.source,
			pkgDir:     tr.pkgDir,
			bins:       bins,
			pin:        r.job.pin(),
			version:    config.NormalizeVersion(r.release.TagName),
			binAsset:   binAsset,
			fontAssets: fontAssets,
		})
	}

	if len(shimPlans) == 0 {
		if hadErrors {
			return errSilent
		}
		return nil
	}

	type shimRow struct{ shim, binary, pkg string }
	var shimRows []shimRow
	for _, p := range shimPlans {
		for shimName, binKey := range p.bins {
			shimRows = append(shimRows, shimRow{shimName, binKey, p.key})
		}
	}
	if len(shimRows) > 0 {
		slices.SortFunc(shimRows, func(a, b shimRow) int { return strings.Compare(a.shim, b.shim) })
		rows := make([][]string, len(shimRows))
		for i, r := range shimRows {
			rows[i] = []string{r.pkg, r.binary, r.shim}
		}
		printTable([]string{"name", "bin", "target"}, rows, nil)
	}

	totalFonts := 0
	for _, p := range shimPlans {
		for _, fonts := range p.fontAssets {
			totalFonts += len(fonts)
		}
	}
	var confirmMsg string
	switch {
	case len(shimRows) > 0 && totalFonts > 0:
		confirmMsg = fmt.Sprintf("create %d bin(s) and %d font(s)", len(shimRows), totalFonts)
	case len(shimRows) > 0:
		confirmMsg = fmt.Sprintf("create %d bin(s)", len(shimRows))
	default:
		confirmMsg = fmt.Sprintf("install %d font(s)", totalFonts)
	}
	sep()
	if !promptConfirm(confirmMsg) {
		if hadErrors {
			return errSilent
		}
		return nil
	}
	hasOutput = false

	for _, p := range shimPlans {
		printTitle(p.jobName)
		if forceInstall {
			if existing, ok := manifest.Extracts[p.key]; ok {
				for shimName := range existing.AllBins() {
					_ = shim.Remove(shimName)
				}
			}
		}
		manifest.Repos[p.jobName] = p.source
		assets := make(map[string]config.AssetEntry)
		if p.binAsset != "" && len(p.bins) > 0 {
			assets[p.binAsset] = config.AssetEntry{Bin: p.bins}
		}
		for assetName, ae := range p.fontAssets {
			assets[assetName] = config.AssetEntry{Font: ae}
		}
		manifest.Extracts[p.key] = config.PackageEntry{
			Pin:     p.pin,
			Version: p.version,
			Asset:   assets,
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
		fontFailed := false
		if len(p.fontAssets) > 0 {
			fontsDir, err := userFontDir()
			if err != nil {
				printFail(cfg, "font dir: %v", err)
				fontFailed = true
				hadErrors = true
			} else if err := os.MkdirAll(fontsDir, 0755); err != nil {
				printFail(cfg, "font dir: %v", err)
				fontFailed = true
				hadErrors = true
			}
			if !fontFailed {
				for _, fonts := range p.fontAssets {
					fontNames := make([]string, 0, len(fonts))
					for name := range fonts {
						fontNames = append(fontNames, name)
					}
					slices.Sort(fontNames)
					for _, fontName := range fontNames {
						fontPath := fonts[fontName]
						srcPath := filepath.Join(p.pkgDir, filepath.FromSlash(fontPath))
						if err := installFont(srcPath, fontsDir); err != nil {
							printFail(cfg, "font %s: %v", fontName, err)
							hadErrors = true
						} else {
							printPass(cfg, "font %s", fontName)
						}
					}
				}
			}
		}
		if !shimFailed && len(p.bins) > 0 {
			printPass(cfg, "installed %s", p.version)
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
	rows := make([][]string, len(ready))
	for i, r := range ready {
		assetNames := make([]string, len(r.chosens))
		for j, c := range r.chosens {
			assetNames[j] = c.Name
		}
		rows[i] = []string{r.job.key(), r.job.pin(), config.NormalizeVersion(r.release.TagName), strings.Join(assetNames, ", "), r.job.source}
	}
	colors := []func(string) string{nil, nil, colorfn(cfg, "new"), nil, nil}
	printTable([]string{"name", "pin", "update", "asset", "repo"}, rows, colors)
	sep()
	return promptConfirm(fmt.Sprintf("install %d package(s)", len(ready)))
}

func fontKeySet(candidates []asset.FontCandidate) map[string]bool {
	s := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		s[c.Key()] = true
	}
	return s
}

func binKeySet(candidates []asset.BinaryCandidate) map[string]bool {
	s := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		s[c.Key()] = true
	}
	return s
}

func filterNewFonts(all []asset.FontCandidate, prev map[string]bool) []asset.FontCandidate {
	var result []asset.FontCandidate
	for _, c := range all {
		if !prev[c.Key()] {
			result = append(result, c)
		}
	}
	return result
}

func filterNewBins(all []asset.BinaryCandidate, prev map[string]bool) []asset.BinaryCandidate {
	var result []asset.BinaryCandidate
	for _, c := range all {
		if !prev[c.Key()] {
			result = append(result, c)
		}
	}
	return result
}
