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

	var resolved []jobWithRelease
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

		resolved = append(resolved, jobWithRelease{
			job:     installJob{name: pkgName, source: source, version: ver, pinned: pinned},
			release: rel,
		})
	}

	if len(resolved) == 0 {
		if hadErrors {
			return errSilent
		}
		return nil
	}

	// Intro gate: show the resolved packages and let the user bail before any
	// asset prompt or download. No asset column — assets aren't chosen until
	// after opt-in.
	introRows := make([][]string, 0, len(resolved))
	for _, r := range resolved {
		introRows = append(introRows, []string{r.job.key(), config.NormalizeVersion(r.release.TagName), r.job.pin(), r.job.source})
	}
	if !gate([]string{"name", "version", "pin", "repo"}, introRows, []func(string) string{nil, colorfn(cfg, "new"), nil, nil}, fmt.Sprintf("add %d package(s)", len(resolved))) {
		return nil
	}

	// After opt-in, resolve each package's asset(s), prompting only where the
	// choice is ambiguous. A skipped package drops out.
	var ready []jobWithRelease
	for _, r := range resolved {
		ac, err := asset.SelectAssetAuto(r.release.Assets, cfg, "", r.job.name)
		if err != nil {
			printFail(cfg, "%s: %v", r.job.name, err)
			hadErrors = true
			continue
		}
		chosens, err := asset.PromptAssetsMulti(ac, r.job.name)
		if errors.Is(err, asset.ErrSkip) {
			continue
		}
		if err != nil {
			printFail(cfg, "%s: %v", r.job.name, err)
			hadErrors = true
			continue
		}
		r.chosens = chosens
		ready = append(ready, r)
	}

	if len(ready) == 0 {
		if hadErrors {
			return errSilent
		}
		return nil
	}

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
				ex, err := downloadAndExtract(ctx, ghClient, dirs, owner, repo, r.release.TagName, cacheDir, r.job.name, r.job.key(), ver, r.job.name, r.chosens)
				if err != nil {
					return installTaskResult{}, err
				}
				return installTaskResult{r: r, extractResult: ex}, nil
			},
		}
	}

	installResults := parallel.Run(cmd.Context(), installTasks, cfg.NumParallel)

	type shimPlan struct {
		key     string
		jobName string
		source  string
		pkgDir  string
		assets  []string
		bin     map[string]string
		font    map[string]string
		pin     string
		version string
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

		if len(tr.bins) == 0 && len(tr.fonts) == 0 {
			printFail(cfg, "%s: no binaries or fonts found in %s", r.job.name, strings.Join(assetNames(r.chosens), ", "))
			hadErrors = true
			continue
		}

		var bin map[string]string
		if len(tr.bins) > 0 {
			key := r.job.key()
			_, _, pinned := config.ParseVersionSuffix(key)

			selected, discoverErr := asset.SelectBins(tr.bins, nil, r.job.name)
			if errors.Is(discoverErr, asset.ErrSkip) {
				continue
			}
			if len(selected) > 0 {
				rawKeys := make([]string, len(selected))
				proposed := proposedShimNames(key, selected)
				for i, s := range selected {
					print("%s: found bin [%s]", r.job.name, s.Key())
					rawKeys[i] = s.Key()
				}

				reserved := reservedShimNames(manifest, r.job.name)
				for _, prev := range shimPlans {
					if prev.jobName == r.job.name {
						continue
					}
					for shimName := range prev.bin {
						reserved[shimName] = prev.jobName
					}
				}
				shimNames := proposed
				if hasReservedConflict(proposed, reserved) || (!pinned && needsShimRenamePrompt(r.job.name, selected)) {
					renamed, promptErr := asset.PromptBinNames(rawKeys, proposed, reserved, r.job.name)
					if errors.Is(promptErr, asset.ErrSkip) {
						continue
					}
					if renamed != nil {
						shimNames = renamed
					}
				}
				bin = make(map[string]string, len(selected))
				for i, s := range selected {
					bin[shimNames[i]] = s.Key()
				}
			}
		}

		var font map[string]string
		if len(tr.fonts) > 0 {
			fontReserved := reservedFontNames(manifest, r.job.name)
			for _, prev := range shimPlans {
				if prev.jobName == r.job.name {
					continue
				}
				for fontName := range prev.font {
					fontReserved[fontName] = prev.jobName
				}
			}
			selectedFonts, selErr := asset.SelectFonts(tr.fonts, nil, r.job.name)
			if selErr == nil && len(selectedFonts) > 0 {
				namedFonts, promptErr := asset.PromptFontNames(selectedFonts, fontReserved, r.job.name)
				if !errors.Is(promptErr, asset.ErrSkip) && len(namedFonts) > 0 {
					font = namedFonts
					fontNames := make([]string, 0, len(namedFonts))
					for k := range namedFonts {
						fontNames = append(fontNames, k)
					}
					slices.Sort(fontNames)
					for _, fontName := range fontNames {
						print("%s: found font [%s]", r.job.name, fontName)
					}
				}
			}
		}

		if len(bin) == 0 && len(font) == 0 {
			continue
		}

		shimPlans = append(shimPlans, shimPlan{
			key:     r.job.key(),
			jobName: r.job.name,
			source:  r.job.source,
			pkgDir:  tr.pkgDir,
			assets:  assetNames(r.chosens),
			bin:     bin,
			font:    font,
			pin:     r.job.pin(),
			version: config.NormalizeVersion(r.release.TagName),
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
		for shimName, binKey := range p.bin {
			tableRows = append(tableRows, installRow{p.key, p.version, binKey, shimName})
		}
		for fontName, fontPath := range p.font {
			tableRows = append(tableRows, installRow{p.key, p.version, fontPath, fontName})
			totalFonts++
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

	successCount := 0
	for _, p := range shimPlans {
		if forceInstall {
			if existing, ok := manifest.Extracts[p.key]; ok {
				for shimName := range existing.AllBins() {
					_ = shim.Remove(shimName)
				}
				if oldFonts := existing.AllFonts(); len(oldFonts) > 0 {
					var newPaths []string
					for _, fontPath := range p.font {
						newPaths = append(newPaths, fontPath)
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
		manifest.Extracts[p.key] = config.PackageEntry{
			Pin:     p.pin,
			Version: p.version,
			Assets:  p.assets,
			Bin:     p.bin,
			Font:    p.font,
		}
		shimFailed := false
		for shimName, binsKey := range p.bin {
			binDir, binName := parseBinPath(binsKey)
			if err := shim.Create(shimName, binName, p.pkgDir, binDir); err != nil {
				printFail(cfg, "%s: %s: could not create shim: %v", p.jobName, shimName, err)
				shimFailed = true
				hadErrors = true
			}
		}
		fontFailed := false
		if len(p.font) > 0 {
			fontsDir, err := ensureFontDir()
			if err != nil {
				printFail(cfg, "%s: font dir: %v", p.jobName, err)
				fontFailed = true
				hadErrors = true
			}
			if !fontFailed {
				fontNames := make([]string, 0, len(p.font))
				for fontName := range p.font {
					fontNames = append(fontNames, fontName)
				}
				slices.Sort(fontNames)
				for _, fontName := range fontNames {
					srcPath := filepath.Join(p.pkgDir, filepath.FromSlash(p.font[fontName]))
					if err := installFont(srcPath, fontsDir); err != nil {
						printFail(cfg, "%s: %s: could not install font: %v", p.jobName, fontName, err)
						hadErrors = true
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
