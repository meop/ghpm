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
			fmt.Printf("[dry-run] would remove all cached assets in %s\n", releaseDir)
		} else {
			if !promptConfirm(fmt.Sprintf("remove all cached assets in %s", releaseDir)) {
				return nil
			}
			if err := os.RemoveAll(releaseDir); err != nil {
				printFail(cfg, "%v", err)
				return errSilent
			}
			printPass(cfg, "removed all cached assets")
			_ = os.MkdirAll(releaseDir, 0755)
		}
		cleanBrokenInstalls(cfg, manifest, releaseDir)
		return nil
	}

	b1 := cleanBrokenInstalls(cfg, manifest, releaseDir)
	b2 := cleanOrphanedBinShims(cfg, manifest)
	b3 := cleanOrphanedExtracts(cfg, manifest)
	b4 := cleanOrphanedReleases(cfg, releaseDir, manifest)
	if !b1 && !b2 && !b3 && !b4 {
		printInfo(cfg, "nothing to tidy")
	}
	return nil
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
		display      string
		shimPaths    []string
		extractPath  string
		manifestKey  string
		trimBinNames []string
	}
	var items []item

	for key, pkg := range manifest.Extracts {
		if len(pkg.Bins) == 0 {
			continue
		}
		var shimPaths []string
		var missingShimNames []string
		for shimName := range pkg.Bins { // key is the shim name directly
			sp := filepath.Join(binDir, shimName)
			shimPaths = append(shimPaths, sp)
			if _, err := os.Lstat(sp); os.IsNotExist(err) {
				missingShimNames = append(missingShimNames, shimName)
			}
		}
		_, extractErr := os.Lstat(filepath.Join(pkgsDir, key, pkg.Version))
		extractMissing := os.IsNotExist(extractErr)
		allShimsMissing := len(missingShimNames) == len(pkg.Bins)

		if len(missingShimNames) == 0 && !extractMissing {
			continue
		}
		if allShimsMissing || extractMissing {
			var missing []string
			if allShimsMissing {
				missing = append(missing, fmt.Sprintf("bin (%s)", strings.Join(missingShimNames, ", ")))
			}
			if extractMissing {
				missing = append(missing, "extract")
			}
			items = append(items, item{
				display:     fmt.Sprintf("%s: missing %s", key, strings.Join(missing, ", ")),
				shimPaths:   shimPaths,
				extractPath: filepath.Join(pkgsDir, key, pkg.Version),
				manifestKey: key,
			})
		} else {
			items = append(items, item{
				display:      fmt.Sprintf("%s: missing bin (%s)", key, strings.Join(missingShimNames, ", ")),
				manifestKey:  key,
				trimBinNames: missingShimNames, // these are shim names (map keys)
			})
		}
	}

	if len(items) == 0 {
		return false
	}

	slices.SortFunc(items, func(a, b item) int { return strings.Compare(a.display, b.display) })
	printTitle("broken installs")
	for _, it := range items {
		printWarn(cfg, "%s", it.display)
	}

	if dryRun {
		return true
	}

	if !promptConfirm(fmt.Sprintf("fix %d broken install(s)", len(items))) {
		return true
	}

	manifestTouched := false
	for _, it := range items {
		for _, sp := range it.shimPaths {
			_ = os.Remove(sp)
		}
		if it.extractPath != "" {
			_ = os.RemoveAll(it.extractPath)
			parent := filepath.Dir(it.extractPath)
			if parent != pkgsDir {
				if es, err := os.ReadDir(parent); err == nil && len(es) == 0 {
					_ = os.Remove(parent)
				}
			}
		}
		if it.manifestKey != "" {
			if len(it.trimBinNames) > 0 {
				entry := manifest.Extracts[it.manifestKey]
				for _, name := range it.trimBinNames {
					delete(entry.Bins, name)
				}
				manifest.Extracts[it.manifestKey] = entry
			} else {
				baseName, _, _ := config.ParseVersionSuffix(it.manifestKey)
				pkg := manifest.Extracts[it.manifestKey]
				if src, ok := manifest.Repos[baseName]; ok {
					relPath := strings.ReplaceAll(src, "/", string(filepath.Separator))
					downloadVersionDir := filepath.Join(releaseDir, relPath, pkg.Version)
					_ = os.RemoveAll(downloadVersionDir)
					parent := filepath.Join(releaseDir, relPath)
					for parent != releaseDir {
						es, err := os.ReadDir(parent)
						if err != nil || len(es) > 0 {
							break
						}
						_ = os.Remove(parent)
						parent = filepath.Dir(parent)
					}
				}
				delete(manifest.Extracts, it.manifestKey)
				hasOther := false
				for k := range manifest.Extracts {
					if n, _, _ := config.ParseVersionSuffix(k); n == baseName {
						hasOther = true
						break
					}
				}
				if !hasOther {
					delete(manifest.Repos, baseName)
				}
			}
			manifestTouched = true
		}
	}

	if manifestTouched {
		if err := config.SaveManifest(manifest); err != nil {
			printFail(cfg, "could not save manifest: %v", err)
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
		for shimName := range pkg.Bins {
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

	printTitle("orphaned bins")
	for _, d := range displays {
		printWarn(cfg, "%s", d)
	}

	if dryRun {
		return true
	}

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
					displays = append(displays, fmt.Sprintf("%s@%s: missing manifest", key, ve.Name()))
				}
			}
		}
	}

	if len(paths) == 0 {
		return false
	}

	printTitle("orphaned extracts")
	for _, d := range displays {
		printWarn(cfg, "%s", d)
	}

	if dryRun {
		return true
	}

	if !promptConfirm(fmt.Sprintf("remove %d orphaned extract(s)", len(paths))) {
		return true
	}

	for _, p := range paths {
		_ = os.RemoveAll(p)
		parent := filepath.Dir(p)
		if parent != pkgsDir {
			if es, err := os.ReadDir(parent); err == nil && len(es) == 0 {
				_ = os.Remove(parent)
			}
		}
	}
	return true
}

func cleanOrphanedReleases(cfg *config.Settings, releaseDir string, manifest *config.Manifest) bool {
	installed := map[string]bool{}
	for key, pkg := range manifest.Extracts {
		name, _, _ := config.ParseVersionSuffix(key)
		if src, ok := manifest.Repos[name]; ok {
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

	printTitle("orphaned downloads")
	for _, p := range toRemove {
		rel, _ := filepath.Rel(releaseDir, p)
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) >= 5 {
			printWarn(cfg, "%s: unused download (%s|%s)", parts[2], parts[3], parts[len(parts)-1])
		} else {
			printWarn(cfg, "%s", rel)
		}
	}

	if dryRun {
		return true
	}

	if !promptConfirm(fmt.Sprintf("remove %d unused download(s)", len(toRemove))) {
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
