package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/asset"
	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/store"
)

var noVerify bool

const msgAllUpToDate = "all packages are up to date"

type cmdInit struct {
	cfg      *config.Settings
	manifest *config.Manifest
	repos    map[string]string
	unlock   func()
	gh       gh.Client
	dirs     store.Dirs
}

// extractResult is the shared per-package output of a parallel download+extract
// task: which directory each asset extracted to, plus discovered fonts and bins.
type extractResult struct {
	pkgDirByAsset map[string]string
	fontsByAsset  map[string][]asset.FontCandidate
	binsByAsset   map[string][]asset.BinCandidate
}

type cmdOptions struct {
	Lock     bool
	Manifest bool
	GH       bool
	Dirs     bool
	Repos    bool
	NoVerify bool
}

func initCommand(opts cmdOptions) (*cmdInit, error) {
	ci := &cmdInit{dirs: store.NewLocalDirs()}

	if opts.Lock {
		unlock, err := config.AcquireLock()
		if err != nil {
			printFail(nil, "%v", err)
			return nil, errSilent
		}
		ci.unlock = unlock
	}

	cfg, err := config.LoadSettings()
	if err != nil {
		printFail(nil, "could not load settings: %v", err)
		return nil, ci.fail()
	}
	ci.cfg = cfg
	if opts.NoVerify && cfg.NoVerify {
		noVerify = true
	}

	if opts.Manifest {
		manifest, err := config.LoadManifest()
		if err != nil {
			printFail(cfg, "could not load manifest: %v", err)
			return nil, ci.fail()
		}
		ci.manifest = manifest
	}

	if opts.Dirs {
		if err := config.EnsureDirs(); err != nil {
			printFail(cfg, "%v", err)
			return nil, ci.fail()
		}
	}

	if opts.GH {
		if err := gh.CheckInstalled(); err != nil {
			printFail(cfg, "%v", err)
			return nil, ci.fail()
		}
		ci.gh = gh.NewCLI()
	}

	if opts.Repos {
		repos, warnings, repoErr := config.LoadRepos()
		if repoErr != nil {
			printInfo(cfg, "could not load repos: %v", repoErr)
		}
		for _, w := range warnings {
			printWarn(cfg, "%s", w)
		}
		ci.repos = repos
	}

	return ci, nil
}

func (ci *cmdInit) fail() error {
	if ci.unlock != nil {
		ci.unlock()
	}
	return errSilent
}

func (ci *cmdInit) close() {
	if ci.unlock != nil {
		ci.unlock()
	}
}

func addSkipVerifyFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&noVerify, "skip-verify", false, "Skip SHA256 verification")
}

func addNameFormatFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVarP(&longNames, "long-names", "l", false, "Print names only, one per line")
	cmd.Flags().BoolVarP(&shortNames, "short-names", "s", false, "Print names only, space-separated on one line")
}

func saveManifest(cfg *config.Settings, manifest *config.Manifest) error {
	if err := config.SaveManifest(manifest); err != nil {
		printFail(cfg, "could not save manifest: %v", err)
		return errSilent
	}
	return nil
}

const msgNoBinaryFound = "no binary found in %s"

func printRateLimited(cfg *config.Settings, key string) {
	printWarn(cfg, "%s: rate limited", key)
}

func printRateLimitSummary(cfg *config.Settings, checked, total, skipped int) {
	printWarn(cfg, "checked %d/%d packages (%d skipped due to rate limiting)", checked, total, skipped)
}

// appendAssetEntryRows appends one row per bin or font in ae. prefix provides
// the leading columns; type, artifact, and path are appended for each entry.
func appendAssetEntryRows(rows [][]string, prefix []string, ae config.AssetEntry) [][]string {
	if ae.IsBin() {
		shimNames := make([]string, 0, len(ae.Bin))
		for s := range ae.Bin {
			shimNames = append(shimNames, s)
		}
		slices.Sort(shimNames)
		for _, shimName := range shimNames {
			rows = append(rows, append(append([]string(nil), prefix...), "bin", shimName, ae.Bin[shimName]))
		}
	}
	if ae.IsFont() {
		fontNames := make([]string, 0, len(ae.Font))
		for f := range ae.Font {
			fontNames = append(fontNames, f)
		}
		slices.Sort(fontNames)
		for _, fontName := range fontNames {
			rows = append(rows, append(append([]string(nil), prefix...), "font", fontName, ae.Font[fontName]))
		}
	}
	return rows
}

// downloadAndExtract downloads, verifies, and extracts each chosen asset for a
// package. displayName is used in progress messages; extractKey and version
// identify the extract directory; pkgName is used for binary discovery.
func downloadAndExtract(
	ctx context.Context,
	cfg *config.Settings,
	ghClient gh.Client,
	dirs store.Dirs,
	owner, repo, tagName, cacheDir, displayName, extractKey, ver, pkgName string,
	chosens []gh.Asset,
) (extractResult, error) {
	pkgDirByAsset := make(map[string]string, len(chosens))
	fontsByAsset := make(map[string][]asset.FontCandidate)
	binsByAsset := make(map[string][]asset.BinCandidate)

	for _, chosen := range chosens {
		if _, err := os.Stat(filepath.Join(cacheDir, chosen.Name)); os.IsNotExist(err) {
			printInfo(cfg, "%s: downloading %s...", displayName, chosen.Name)
			if err := ghClient.DownloadAsset(ctx, owner, repo, tagName, chosen.Name, cacheDir); err != nil {
				return extractResult{}, err
			}
		}
		if !noVerify {
			verified, err := ghClient.VerifyAsset(ctx, owner, repo, tagName, cacheDir, chosen.Name)
			if err != nil {
				return extractResult{}, err
			}
			if !verified {
				printWarn(cfg, "%s: %s unverified (no attestation)", displayName, chosen.Name)
			}
		}

		assetDir, err := dirs.ExtractDir(extractKey, ver, chosen.Name)
		if err != nil {
			return extractResult{}, err
		}
		if err := os.RemoveAll(assetDir); err != nil {
			return extractResult{}, err
		}
		if err := os.MkdirAll(assetDir, 0755); err != nil {
			return extractResult{}, err
		}
		if err := asset.ExtractPackage(cacheDir, chosen.Name, assetDir); err != nil {
			_ = os.RemoveAll(assetDir)
			return extractResult{}, err
		}
		pkgDirByAsset[chosen.Name] = assetDir

		if fonts := asset.FindFonts(assetDir); len(fonts) > 0 {
			fontsByAsset[chosen.Name] = fonts
		}
		if bins := asset.FindBins(assetDir, pkgName); len(bins) > 0 {
			binsByAsset[chosen.Name] = bins
		}
	}

	return extractResult{
		pkgDirByAsset: pkgDirByAsset,
		fontsByAsset:  fontsByAsset,
		binsByAsset:   binsByAsset,
	}, nil
}

// buildBatchItems constructs version-check batch items for non-fixed extracts,
// resolving each package's source from repos and parsing any pin constraint.
func buildBatchItems(extracts map[string]config.PackageEntry, repos map[string]string) []gh.BatchItem {
	items := make([]gh.BatchItem, 0, len(extracts))
	for key, pkg := range extracts {
		if pkg.Pin == "fixed" {
			continue
		}
		pkgName, verStr, isPinned := config.ParseVersionSuffix(key)
		var c config.Constraint
		if isPinned {
			parsed, err := config.ParseConstraint(verStr)
			if err != nil {
				continue
			}
			c = parsed
		}
		items = append(items, gh.BatchItem{
			Key:    key,
			Source: repos[pkgName],
			Pin:    c,
		})
	}
	return items
}

// printNameList prints names in long or short format. Returns true if either flag was set.
func printNameList(names []string) bool {
	if longNames {
		for _, n := range names {
			fmt.Println(n)
		}
		return true
	}
	if shortNames {
		fmt.Println(strings.Join(names, " "))
		return true
	}
	return false
}
