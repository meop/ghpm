package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/store"
)

func newTidyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "tidy",
		Aliases: []string{"ti", "cl", "clean"},
		Short:   "Clean unused downloads and orphaned items",
		Args:    cobra.NoArgs,
		RunE:    runTidy,
	}
	cmd.Flags().Bool("all", false, "remove all cached assets regardless of installation status")
	return cmd
}

func runTidy(cmd *cobra.Command, args []string) error {
	ci, err := initCommand(cmdOptions{Lock: true, Manifest: true})
	if err != nil {
		return err
	}
	defer ci.close()
	cfg := ci.cfg
	manifest := ci.manifest

	all, _ := cmd.Flags().GetBool("all")
	releaseDir, err := store.ReleaseBaseDir()
	if err != nil {
		printFail(cfg, "%v", err)
		return errSilent
	}

	if all {
		if dryRun {
			fmt.Printf("remove all cached assets in %s\n", releaseDir)
		} else {
			sep()
			if !promptConfirm("remove all download(s)") {
				return nil
			}
			if err := os.RemoveAll(releaseDir); err != nil {
				printFail(cfg, "%v", err)
				return errSilent
			}
			printPass(cfg, "removed all cached assets")
			_ = os.MkdirAll(releaseDir, 0755)
		}
		return nil
	}

	b1 := cleanBrokenInstalls(cfg, manifest, releaseDir)
	b2 := cleanOrphanedBinShims(cfg, manifest)
	b3 := cleanOrphanedFonts(cfg, manifest, releaseDir)
	b4 := cleanOrphanedExtracts(cfg, manifest)
	b5 := cleanOrphanedReleases(cfg, releaseDir, manifest)
	if !b1 && !b2 && !b3 && !b4 && !b5 {
		print("nothing to tidy")
	}
	return nil
}

func pruneExtract(path, pkgsDir string) {
	_ = os.RemoveAll(path)
	parent := filepath.Dir(path)
	if parent != pkgsDir {
		if es, err := os.ReadDir(parent); err == nil && len(es) == 0 {
			_ = os.Remove(parent)
		}
	}
}

// cleanBrokenInstalls removes manifest entries whose shim or extract is missing,
// and trims BinNames for entries where only some shims are missing.
func cleanBrokenInstalls(cfg *config.Settings, manifest *config.Manifest, releaseDir string) bool {
	binDir, err := store.BinDir()
	if err != nil {
		return false
	}
	pkgsDir, err := store.ExtractsDir()
	if err != nil {
		return false
	}

	type item struct {
		display       string
		shimPaths     []string
		extractPath   string
		manifestKey   string
		trimShimNames []string
	}
	var items []item

	for key, pkg := range manifest.Extracts {
		allBins := pkg.AllBins()
		allFonts := pkg.AllFonts()

		if len(allBins) == 0 && len(allFonts) > 0 {
			for assetName := range pkg.Asset {
				if _, err := os.Lstat(filepath.Join(pkgsDir, key, pkg.Version, assetName)); os.IsNotExist(err) {
					items = append(items, item{
						display:     fmt.Sprintf("%s: missing extract", key),
						extractPath: filepath.Join(pkgsDir, key, pkg.Version),
						manifestKey: key,
					})
					break
				}
			}
			continue
		}

		if len(allBins) == 0 {
			continue
		}
		var shimPaths []string
		var missingShimNames []string
		for shimName := range allBins {
			sp := filepath.Join(binDir, shimName)
			shimPaths = append(shimPaths, sp)
			if _, err := os.Lstat(sp); os.IsNotExist(err) {
				missingShimNames = append(missingShimNames, shimName)
			}
		}
		extractMissing := false
		for assetName := range pkg.Asset {
			if _, err := os.Lstat(filepath.Join(pkgsDir, key, pkg.Version, assetName)); os.IsNotExist(err) {
				extractMissing = true
				break
			}
		}
		if len(missingShimNames) == 0 && !extractMissing {
			continue
		}
		if extractMissing {
			items = append(items, item{
				display:     fmt.Sprintf("%s: missing extract", key),
				shimPaths:   shimPaths,
				extractPath: filepath.Join(pkgsDir, key, pkg.Version),
				manifestKey: key,
			})
		} else {
			for _, shimName := range missingShimNames {
				items = append(items, item{
					display:       fmt.Sprintf("%s: missing bin (%s)", key, shimName),
					manifestKey:   key,
					trimShimNames: []string{shimName},
				})
			}
		}
	}

	if len(items) == 0 {
		return false
	}

	slices.SortFunc(items, func(a, b item) int { return strings.Compare(a.display, b.display) })
	printTitle("broken install(s)")
	for _, it := range items {
		printWarn(cfg, "%s", it.display)
	}

	if dryRun {
		return true
	}

	sep()
	if !promptConfirm(fmt.Sprintf("remove %d broken install(s)", len(items))) {
		return true
	}

	manifestTouched := false
	for _, it := range items {
		for _, sp := range it.shimPaths {
			_ = os.Remove(sp)
		}
		if it.extractPath != "" {
			pruneExtract(it.extractPath, pkgsDir)
		}
		if it.manifestKey != "" {
			if len(it.trimShimNames) > 0 {
				entry := manifest.Extracts[it.manifestKey]
				for assetName, ae := range entry.Asset {
					for _, shimName := range it.trimShimNames {
						delete(ae.Bin, shimName)
					}
					entry.Asset[assetName] = ae
				}
				manifest.Extracts[it.manifestKey] = entry
				if len(entry.AllBins()) == 0 && len(entry.AllFonts()) == 0 {
					manifest.RemoveExtract(it.manifestKey)
					pruneExtract(filepath.Join(pkgsDir, it.manifestKey, entry.Version), pkgsDir)
				}
			} else {
				baseName, _, _ := config.ParseVersionSuffix(it.manifestKey)
				pkg := manifest.Extracts[it.manifestKey]
				if src, ok := manifest.Repos[baseName]; ok {
					downloadVersionDir := filepath.Join(releaseDir, store.SourceToRelPath(src), pkg.Version)
					_ = os.RemoveAll(downloadVersionDir)
					parent := filepath.Join(releaseDir, store.SourceToRelPath(src))
					for parent != releaseDir {
						es, err := os.ReadDir(parent)
						if err != nil || len(es) > 0 {
							break
						}
						_ = os.Remove(parent)
						parent = filepath.Dir(parent)
					}
				}
				manifest.RemoveExtract(it.manifestKey)
			}
			manifestTouched = true
		}
	}

	if manifestTouched {
		if err := saveManifest(cfg, manifest); err != nil {
			return true
		}
	}
	return true
}

// cleanOrphanedFonts removes manifest font entries that are not (fully) installed in the
// user font directory or registry. Because the font is tracked in the manifest, ghpm owns it
// and may also clean up any partial remnants (file or registry entry) left behind.
func cleanOrphanedFonts(cfg *config.Settings, manifest *config.Manifest, releaseDir string) bool {
	fontsDir, err := userFontDir()
	if err != nil {
		return false
	}
	pkgsDir, err := store.ExtractsDir()
	if err != nil {
		return false
	}

	type fontItem struct {
		display     string
		manifestKey string
		fontName    string
		fontFile    string
	}
	var items []fontItem

	for key, pkg := range manifest.Extracts {
		for _, ae := range pkg.Asset {
			for fontName, fontPath := range ae.Font {
				if !fontInstalled(fontPath, fontsDir) {
					items = append(items, fontItem{
						display:     fmt.Sprintf("%s: orphaned font (%s)", key, fontName),
						manifestKey: key,
						fontName:    fontName,
						fontFile:    filepath.Base(fontPath),
					})
				}
			}
		}
	}

	if len(items) == 0 {
		return false
	}

	slices.SortFunc(items, func(a, b fontItem) int { return strings.Compare(a.display, b.display) })
	sep()
	printTitle("orphaned font(s)")
	for _, it := range items {
		printWarn(cfg, "%s", it.display)
	}

	if dryRun {
		return true
	}

	sep()
	if !promptConfirm(fmt.Sprintf("remove %d orphaned font(s)", len(items))) {
		return true
	}

	manifestTouched := false
	for _, it := range items {
		_ = os.Remove(filepath.Join(fontsDir, it.fontFile))
		unregisterFont(it.fontFile)

		entry, ok := manifest.Extracts[it.manifestKey]
		if !ok {
			continue
		}
		for assetName, ae := range entry.Asset {
			delete(ae.Font, it.fontName)
			entry.Asset[assetName] = ae
		}
		manifest.Extracts[it.manifestKey] = entry
		if len(entry.AllBins()) == 0 && len(entry.AllFonts()) == 0 {
			baseName, _, _ := config.ParseVersionSuffix(it.manifestKey)
			if src, ok := manifest.Repos[baseName]; ok {
				downloadVersionDir := filepath.Join(releaseDir, store.SourceToRelPath(src), entry.Version)
				_ = os.RemoveAll(downloadVersionDir)
				parent := filepath.Join(releaseDir, store.SourceToRelPath(src))
				for parent != releaseDir {
					es, err := os.ReadDir(parent)
					if err != nil || len(es) > 0 {
						break
					}
					_ = os.Remove(parent)
					parent = filepath.Dir(parent)
				}
			}
			manifest.RemoveExtract(it.manifestKey)
			pruneExtract(filepath.Join(pkgsDir, it.manifestKey, entry.Version), pkgsDir)
		}
		manifestTouched = true
	}

	if manifestTouched {
		if err := saveManifest(cfg, manifest); err != nil {
			return true
		}
	}
	return true
}

// cleanOrphanedBinShims removes files in bin/ that have no corresponding manifest entry.
func cleanOrphanedBinShims(cfg *config.Settings, manifest *config.Manifest) bool {
	binDir, err := store.BinDir()
	if err != nil {
		return false
	}

	expected := map[string]bool{exeName(binGh): true, exeName(binGhpm): true}
	for _, pkg := range manifest.Extracts {
		for shimName := range pkg.AllBins() {
			expected[shimName] = true
		}
	}

	var paths []string
	var displays []string
	if binEntries, err := os.ReadDir(binDir); err == nil {
		for _, e := range binEntries {
			if !expected[e.Name()] {
				paths = append(paths, filepath.Join(binDir, e.Name()))
				displays = append(displays, fmt.Sprintf("%s: missing manifest", e.Name()))
			}
		}
	}

	if len(paths) == 0 {
		return false
	}

	printTitle("orphaned bin(s)")
	for _, d := range displays {
		printWarn(cfg, "%s", d)
	}

	if dryRun {
		return true
	}

	sep()
	if !promptConfirm(fmt.Sprintf("remove %d orphaned bin(s)", len(paths))) {
		return true
	}

	for _, p := range paths {
		_ = os.Remove(p)
	}
	return true
}

// cleanOrphanedExtracts removes extract dirs (or stale version subdirs) with no manifest entry.
func cleanOrphanedExtracts(cfg *config.Settings, manifest *config.Manifest) bool {
	pkgsDir, err := store.ExtractsDir()
	if err != nil {
		return false
	}

	var paths []string
	var displays []string

	if pkgEntries, err := os.ReadDir(pkgsDir); err == nil {
		for _, e := range pkgEntries {
			if !e.IsDir() {
				continue
			}
			key := e.Name()
			pkg, inManifest := manifest.Extracts[key]
			if !inManifest {
				paths = append(paths, filepath.Join(pkgsDir, key))
				displays = append(displays, fmt.Sprintf("%s: missing manifest", key))
				continue
			}
			verEntries, err := os.ReadDir(filepath.Join(pkgsDir, key))
			if err != nil {
				continue
			}
			for _, ve := range verEntries {
				if ve.IsDir() && ve.Name() != pkg.Version {
					paths = append(paths, filepath.Join(pkgsDir, key, ve.Name()))
					displays = append(displays, fmt.Sprintf("%s: missing manifest (%s)", key, ve.Name()))
				}
			}
		}
	}

	if len(paths) == 0 {
		return false
	}

	printTitle("orphaned extract(s)")
	for _, d := range displays {
		printWarn(cfg, "%s", d)
	}

	if dryRun {
		return true
	}

	sep()
	if !promptConfirm(fmt.Sprintf("remove %d orphaned extract(s)", len(paths))) {
		return true
	}

	for _, p := range paths {
		pruneExtract(p, pkgsDir)
	}
	return true
}

func cleanOrphanedReleases(cfg *config.Settings, releaseDir string, manifest *config.Manifest) bool {
	installed := map[string]bool{}
	for key, pkg := range manifest.Extracts {
		pkgName, _, _ := config.ParseVersionSuffix(key)
		if src, ok := manifest.Repos[pkgName]; ok {
			installed[src+"/"+pkg.Version] = true
		}
	}

	var toRemove []string
	_ = filepath.WalkDir(releaseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(releaseDir, path)
		if err != nil {
			return nil
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) < 5 {
			return nil
		}
		source := store.SourceFromPath(rel)
		ver := parts[3]
		if !installed[source+"/"+ver] {
			toRemove = append(toRemove, path)
		}
		return nil
	})

	if len(toRemove) == 0 {
		return false
	}

	printTitle("orphaned download(s)")
	for _, p := range toRemove {
		rel, _ := filepath.Rel(releaseDir, p)
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) >= 5 {
			printWarn(cfg, "%s: orphaned download (%s|%s)", parts[2], parts[3], parts[len(parts)-1])
		} else {
			printWarn(cfg, "%s", rel)
		}
	}

	if dryRun {
		return true
	}

	sep()
	if !promptConfirm(fmt.Sprintf("remove %d orphaned download(s)", len(toRemove))) {
		return true
	}

	for _, p := range toRemove {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			printFail(cfg, "%s: %v", p, err)
		}
	}
	var dirs []string
	_ = filepath.WalkDir(releaseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || path == releaseDir || !d.IsDir() {
			return nil
		}
		dirs = append(dirs, path)
		return nil
	})
	for i := len(dirs) - 1; i >= 0; i-- {
		entries, _ := os.ReadDir(dirs[i])
		if len(entries) == 0 {
			_ = os.Remove(dirs[i])
		}
	}
	return true
}
