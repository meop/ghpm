package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	unlock, err := config.AcquireLock()
	if err != nil {
		printFail(nil, "%v", err)
		return errSilent
	}
	defer unlock()

	cfg, err := config.LoadSettings()
	if err != nil {
		printFail(nil, "could not load settings: %v", err)
		return errSilent
	}

	all, _ := cmd.Flags().GetBool("all")
	releaseDir, err := store.ReleaseBaseDir()
	if err != nil {
		printFail(cfg, "%v", err)
		return errSilent
	}

	manifest, err := config.LoadManifest()
	if err != nil {
		printFail(cfg, "could not load manifest: %v", err)
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
	} else {
		if !cleanBrokenLinkage(cfg, manifest, releaseDir) &&
			!cleanOrphanedReleases(cfg, releaseDir, manifest) {
			printInfo(cfg, "nothing to tidy")
		}
		return nil
	}

	cleanBrokenLinkage(cfg, manifest, releaseDir)

	return nil
}

// cleanBrokenLinkage finds and removes all broken links in the bin/extract/manifest
// triangle: manifest entries missing their shim or extract, shims with no manifest
// entry, and extract dirs with no manifest entry.
func cleanBrokenLinkage(cfg *config.Settings, manifest *config.Manifest, releaseDir string) bool {
	binDir, err := store.BinDir()
	if err != nil {
		return false
	}
	pkgsDir, err := store.ExtractsDir()
	if err != nil {
		return false
	}

	type item struct {
		display     string
		shimPaths   []string
		extractPath string
		manifestKey string
	}
	var items []item

	// Manifest entries with missing shim or extract
	for key, pkg := range manifest.Extracts {
		if len(pkg.BinNames) == 0 {
			continue
		}
		var shimPaths []string
		anyMissing := false
		for _, binName := range pkg.BinNames {
			sn := binShimName(key, binName)
			if runtime.GOOS == "windows" {
				sn += ".cmd"
			}
			sp := filepath.Join(binDir, sn)
			shimPaths = append(shimPaths, sp)
			if _, err := os.Lstat(sp); os.IsNotExist(err) {
				anyMissing = true
			}
		}
		_, extractErr := os.Lstat(filepath.Join(pkgsDir, key, pkg.Version))
		if !anyMissing && !os.IsNotExist(extractErr) {
			continue
		}
		var missing []string
		if anyMissing {
			missing = append(missing, "shim")
		}
		if os.IsNotExist(extractErr) {
			missing = append(missing, "extract")
		}
		items = append(items, item{
			display:     fmt.Sprintf("%s: missing %s", key, strings.Join(missing, ", ")),
			shimPaths:   shimPaths,
			extractPath: filepath.Join(pkgsDir, key, pkg.Version),
			manifestKey: key,
		})
	}

	// Shims in bin/ with no manifest entry
	expected := map[string]bool{exeName(binGh): true, exeName(binGhpm): true}
	for key, pkg := range manifest.Extracts {
		for _, binName := range pkg.BinNames {
			sn := binShimName(key, binName)
			if runtime.GOOS == "windows" {
				expected[sn+".cmd"] = true
			} else {
				expected[sn] = true
			}
		}
	}
	if binEntries, err := os.ReadDir(binDir); err == nil {
		for _, e := range binEntries {
			if !expected[e.Name()] {
				items = append(items, item{
					display:   fmt.Sprintf("%s: missing manifest", e.Name()),
					shimPaths: []string{filepath.Join(binDir, e.Name())},
				})
			}
		}
	}

	// Extract dirs (or version subdirs) with no manifest entry
	if pkgEntries, err := os.ReadDir(pkgsDir); err == nil {
		for _, e := range pkgEntries {
			if !e.IsDir() {
				continue
			}
			key := e.Name()
			pkg, inManifest := manifest.Extracts[key]
			if !inManifest {
				items = append(items, item{
					display:     fmt.Sprintf("%s: missing manifest", key),
					extractPath: filepath.Join(pkgsDir, key),
				})
				continue
			}
			verEntries, err := os.ReadDir(filepath.Join(pkgsDir, key))
			if err != nil {
				continue
			}
			for _, ve := range verEntries {
				if ve.IsDir() && ve.Name() != pkg.Version {
					items = append(items, item{
						display:     fmt.Sprintf("%s/%s: missing manifest", key, ve.Name()),
						extractPath: filepath.Join(pkgsDir, key, ve.Name()),
					})
				}
			}
		}
	}

	if len(items) == 0 {
		return false
	}

	slices.SortFunc(items, func(a, b item) int { return strings.Compare(a.display, b.display) })

	for _, it := range items {
		fmt.Println(it.display)
	}

	if dryRun {
		return true
	}

	fmt.Println()
	if !promptConfirm(fmt.Sprintf("remove %d item(s)", len(items))) {
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
			manifestTouched = true
		}
	}

	if manifestTouched {
		if err := config.SaveManifest(manifest); err != nil {
			printFail(cfg, "could not save manifest: %v", err)
			return true
		}
	}
	printPass(cfg, "removed %d item(s)", len(items))
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
		source := "github.com/" + parts[1] + "/" + parts[2]
		ver := parts[3]
		if !installed[source+"/"+ver] {
			toRemove = append(toRemove, path)
		}
		return nil
	})

	if len(toRemove) == 0 {
		return false
	}

	for _, p := range toRemove {
		rel, _ := filepath.Rel(releaseDir, p)
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) >= 5 {
			fmt.Printf("%s: unused download %s (%s)\n", parts[2], parts[3], parts[len(parts)-1])
		} else {
			fmt.Printf("%s\n", rel)
		}
	}

	if dryRun {
		return true
	}

	fmt.Println()
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
	printPass(cfg, "removed %d unused download(s)", len(toRemove))
	return true
}
