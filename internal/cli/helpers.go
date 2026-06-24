package cli

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/asset"
	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/store"
	"github.com/meop/ghpm/internal/ui"
)

var skipHashCheck bool

const msgAllUpToDate = "all packages are up to date"

const msgAllComponentsUpToDate = "all components are up to date"

// msgNoMatch is shown when a name filter was given but matched nothing installed,
// to distinguish it from an empty install set ("no packages installed").
const msgNoMatch = "no packages matched"

type cmdInit struct {
	cfg      *config.Settings
	manifest *config.Manifest
	repos    map[string]string
	unlock   func()
	gh       gh.Client
	dirs     store.Dirs
}

// extractResult is the shared per-package output of a parallel download+extract
// task: the single dir all chosen assets were overlaid into, plus the bins and
// fonts discovered across that combined tree.
type extractResult struct {
	pkgDir string
	bins   []asset.BinCandidate
	fonts  []asset.FontCandidate
}

type cmdOptions struct {
	Lock          bool
	Manifest      bool
	GH            bool
	Dirs          bool
	Repos         bool
	SkipHashCheck bool
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
	ui.SetColorResolver(func(role string) func(string) string { return colorfn(cfg, role) })
	if opts.SkipHashCheck && cfg.SkipHashCheck {
		skipHashCheck = true
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
		repos, repoErr := config.LoadRepos()
		if repoErr != nil {
			printFail(cfg, "could not load repos: %v", repoErr)
			return nil, ci.fail()
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

func addSkipHashCheckFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&skipHashCheck, "skip-hash-check", false, "Skip SHA256 hash verification")
}

func verifyDigest(digest, filePath string) error {
	algo, hex, ok := strings.Cut(digest, ":")
	if !ok || algo != "sha256" {
		return fmt.Errorf("unsupported digest format %q", digest)
	}
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := fmt.Sprintf("%x", h.Sum(nil)); got != hex {
		return fmt.Errorf("got %s, want %s", got, hex)
	}
	return nil
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

// appendEntryRows appends one row per bin or font in the package entry. prefix
// provides the leading columns; type, artifact, and target are appended for each
// entry. artifact is the source file path within the extract; target is the shim
// name (for bins) or user-given font name (for fonts).
func appendEntryRows(rows [][]string, prefix []string, p config.PackageEntry) [][]string {
	shimNames := make([]string, 0, len(p.Bin))
	for s := range p.Bin {
		shimNames = append(shimNames, s)
	}
	slices.Sort(shimNames)
	for _, shimName := range shimNames {
		rows = append(rows, append(append([]string(nil), prefix...), p.Bin[shimName], "bin", shimName))
	}
	fontNames := make([]string, 0, len(p.Font))
	for f := range p.Font {
		fontNames = append(fontNames, f)
	}
	slices.Sort(fontNames)
	for _, fontName := range fontNames {
		rows = append(rows, append(append([]string(nil), prefix...), p.Font[fontName], "font", fontName))
	}
	return rows
}

// downloadAndExtract downloads each chosen asset and overlays them all into a
// single extract dir, in the order given (a later asset overwrites a colliding
// path, so "last wins"). Bins and fonts are then discovered once across the
// combined tree. displayName is used in progress messages; extractKey and version
// identify the extract directory; pkgName is used for binary discovery.
func downloadAndExtract(
	ctx context.Context,
	ghClient gh.Client,
	dirs store.Dirs,
	owner, repo, tagName, cacheDir, displayName, extractKey, ver, pkgName string,
	chosens []gh.Asset,
) (extractResult, error) {
	pkgDir, err := dirs.ExtractDir(extractKey, ver)
	if err != nil {
		return extractResult{}, err
	}
	if err := os.RemoveAll(pkgDir); err != nil {
		return extractResult{}, err
	}
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		return extractResult{}, err
	}

	for _, chosen := range chosens {
		assetPath := filepath.Join(cacheDir, chosen.Name)
		if _, err := os.Stat(assetPath); os.IsNotExist(err) {
			print("%s: downloading [%s]...", displayName, chosen.Name)
			if err := ghClient.DownloadAsset(ctx, owner, repo, tagName, chosen.Name, cacheDir); err != nil {
				return extractResult{}, err
			}
		}
		if !skipHashCheck && chosen.Digest != "" {
			if err := verifyDigest(chosen.Digest, assetPath); err != nil {
				return extractResult{}, fmt.Errorf("%s: %s: %w", displayName, chosen.Name, err)
			}
		}

		if err := asset.ExtractPackage(cacheDir, chosen.Name, pkgDir); err != nil {
			_ = os.RemoveAll(pkgDir)
			return extractResult{}, err
		}
	}

	return extractResult{
		pkgDir: pkgDir,
		bins:   asset.FindBins(pkgDir, pkgName),
		fonts:  asset.FindFonts(pkgDir),
	}, nil
}

// assetNames returns the names of the chosen assets in order, used as the
// manifest's ordered asset list (it records the overlay order for re-extraction).
func assetNames(chosens []gh.Asset) []string {
	names := make([]string, len(chosens))
	for i, c := range chosens {
		names[i] = c.Name
	}
	return names
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

// filterExtracts returns the subset of extracts whose key matches one of names.
// A name matches either the full manifest key (the name shown by list/outdated,
// e.g. "fzf@14") or its base name ("fzf"). With no names it returns extracts
// unchanged, so commands list everything by default. Used by list, outdated, and
// sync so a trailing "[name...]" filters their output the same way everywhere.
func filterExtracts(extracts map[string]config.PackageEntry, names []string) map[string]config.PackageEntry {
	if len(names) == 0 {
		return extracts
	}
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	filtered := make(map[string]config.PackageEntry, len(want))
	for k, p := range extracts {
		base, _, _ := config.ParseVersionSuffix(k)
		if want[k] || want[base] {
			filtered[k] = p
		}
	}
	return filtered
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
