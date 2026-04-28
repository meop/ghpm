package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/entrypoint"
	"github.com/meop/ghpm/internal/store"
)

func newCleanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "remove cached release assets and orphaned package dirs",
		Args:  cobra.NoArgs,
		RunE:  runClean,
	}
	cmd.Flags().Bool("all", false, "remove all cached assets regardless of installation status")
	return cmd
}

func runClean(cmd *cobra.Command, args []string) error {
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
				fmt.Println("aborted")
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
		cleanOrphanedReleases(cfg, releaseDir, manifest)
	}

	cleanOrphanedPackages(cfg, manifest)

	if _, err := entrypoint.Generate(manifest); err != nil {
		printWarn(cfg, "could not generate entrypoint: %v", err)
	}

	return nil
}

func cleanOrphanedReleases(cfg *config.Settings, releaseDir string, manifest *config.Manifest) {
	installed := map[string]bool{}
	for key, pkg := range manifest.Installs {
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
		return
	}

	for _, p := range toRemove {
		rel, _ := filepath.Rel(releaseDir, p)
		fmt.Printf("%s\n", rel)
	}

	if dryRun {
		return
	}

	fmt.Println()
	if !promptConfirm(fmt.Sprintf("remove %d cached file(s)", len(toRemove))) {
		fmt.Println("aborted")
		return
	}

	for _, p := range toRemove {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			printFail(cfg, "%s: %v", p, err)
		}
	}
	printPass(cfg, "cleaned %d cached file(s)", len(toRemove))
}

func cleanOrphanedPackages(cfg *config.Settings, manifest *config.Manifest) {
	pkgsDir, err := store.PackagesDir()
	if err != nil {
		return
	}

	entries, err := os.ReadDir(pkgsDir)
	if err != nil {
		if !os.IsNotExist(err) {
			printFail(cfg, "%v", err)
		}
		return
	}

	var orphaned []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, ok := manifest.Installs[e.Name()]; !ok {
			orphaned = append(orphaned, e.Name())
		}
	}

	if len(orphaned) == 0 {
		return
	}

	fmt.Println()
	for _, name := range orphaned {
		fmt.Printf("packages/%s\n", name)
	}

	if dryRun {
		return
	}

	if !promptConfirm(fmt.Sprintf("remove %d orphaned package dir(s)", len(orphaned))) {
		fmt.Println("aborted")
		return
	}

	for _, name := range orphaned {
		p := filepath.Join(pkgsDir, name)
		if err := os.RemoveAll(p); err != nil {
			printFail(cfg, "%s: %v", name, err)
		}
	}
	printPass(cfg, "cleaned %d orphaned package dir(s)", len(orphaned))
}
