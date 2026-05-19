package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
		if !cleanOrphanedReleases(cfg, releaseDir, manifest) &&
			!cleanOrphanedPackages(cfg, manifest) &&
			!cleanOrphanedShims(cfg, manifest) {
			printInfo(cfg, "nothing to tidy")
		}
		return nil
	}

	cleanOrphanedPackages(cfg, manifest)
	cleanOrphanedShims(cfg, manifest)

	return nil
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
		fmt.Printf("%s\n", rel)
	}

	if dryRun {
		return true
	}

	fmt.Println()
	if !promptConfirm(fmt.Sprintf("remove %d cached file(s)", len(toRemove))) {
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
	printPass(cfg, "cleaned %d cached file(s)", len(toRemove))
	return true
}

func cleanOrphanedShims(cfg *config.Settings, manifest *config.Manifest) bool {
	binDir, err := store.BinDir()
	if err != nil {
		return false
	}
	entries, err := os.ReadDir(binDir)
	if err != nil {
		if !os.IsNotExist(err) {
			printFail(cfg, "%v", err)
		}
		return false
	}

	// Build set of expected shim names from manifest keys
	expected := map[string]bool{}
	for key, pkg := range manifest.Extracts {
		if pkg.BinName == "" {
			continue
		}
		if runtime.GOOS == "windows" {
			expected[key+".cmd"] = true
		} else {
			expected[key] = true
		}
	}

	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	expected["gh"+ext] = true
	expected["ghpm"+ext] = true

	var orphaned []string
	for _, e := range entries {
		if !expected[e.Name()] {
			orphaned = append(orphaned, e.Name())
		}
	}
	if len(orphaned) == 0 {
		return false
	}

	fmt.Println()
	for _, name := range orphaned {
		fmt.Printf("bin/%s\n", name)
	}

	if dryRun {
		return true
	}

	if !promptConfirm(fmt.Sprintf("remove %d orphaned shim(s)", len(orphaned))) {
		return true
	}

	for _, name := range orphaned {
		if err := os.Remove(filepath.Join(binDir, name)); err != nil {
			printFail(cfg, "%s: %v", name, err)
		}
	}
	printPass(cfg, "cleaned %d orphaned shim(s)", len(orphaned))
	return true
}

func cleanOrphanedPackages(cfg *config.Settings, manifest *config.Manifest) bool {
	pkgsDir, err := store.ExtractsDir()
	if err != nil {
		return false
	}

	keyEntries, err := os.ReadDir(pkgsDir)
	if err != nil {
		if !os.IsNotExist(err) {
			printFail(cfg, "%v", err)
		}
		return false
	}

	// orphaned is a list of paths relative to pkgsDir to remove
	var orphaned []string
	for _, e := range keyEntries {
		if !e.IsDir() {
			continue
		}
		key := e.Name()
		pkg, inManifest := manifest.Extracts[key]
		if !inManifest {
			orphaned = append(orphaned, key)
			continue
		}
		// Key is in manifest — check for stale version subdirs (e.g. from failed updates)
		verEntries, err := os.ReadDir(filepath.Join(pkgsDir, key))
		if err != nil {
			continue
		}
		for _, ve := range verEntries {
			if ve.IsDir() && ve.Name() != pkg.Version {
				orphaned = append(orphaned, filepath.Join(key, ve.Name()))
			}
		}
	}

	if len(orphaned) == 0 {
		return false
	}

	fmt.Println()
	for _, name := range orphaned {
		fmt.Printf("extract/%s\n", name)
	}

	if dryRun {
		return true
	}

	if !promptConfirm(fmt.Sprintf("remove %d orphaned package dir(s)", len(orphaned))) {
		return true
	}

	for _, name := range orphaned {
		p := filepath.Join(pkgsDir, name)
		if err := os.RemoveAll(p); err != nil {
			printFail(cfg, "%s: %v", name, err)
			continue
		}
		parent := filepath.Dir(p)
		if parent != pkgsDir {
			if entries, err := os.ReadDir(parent); err == nil && len(entries) == 0 {
				_ = os.Remove(parent)
			}
		}
	}
	printPass(cfg, "cleaned %d orphaned package dir(s)", len(orphaned))
	return true
}
