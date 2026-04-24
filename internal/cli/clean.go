package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/store"
)

func newCleanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove cached release assets",
		Args:  cobra.NoArgs,
		RunE:  runClean,
	}
	cmd.Flags().Bool("all", false, "Remove all cached assets regardless of installation status")
	return cmd
}

func runClean(cmd *cobra.Command, args []string) error {
	all, _ := cmd.Flags().GetBool("all")
	releaseDir, err := store.ReleaseBaseDir()
	if err != nil {
		return err
	}

	if all {
		if dryRun {
			fmt.Printf("[dry-run] would remove all cached assets in %s\n", releaseDir)
			return nil
		}
		if !promptConfirm(fmt.Sprintf("Remove all cached assets in %s?", releaseDir)) {
			fmt.Println("Aborted.")
			return nil
		}
		if err := os.RemoveAll(releaseDir); err != nil {
			return err
		}
		color.Green("✓ removed all cached assets")
		return os.MkdirAll(releaseDir, 0755)
	}

	// Remove only assets for versions not currently installed
	manifest, err := config.LoadManifest()
	if err != nil {
		return err
	}

	// Build set of installed (source, version) pairs
	installed := map[string]bool{}
	for key, pkg := range manifest.Installs {
		name, _, _ := config.ParseVersionSuffix(key)
		if src, ok := manifest.Repos[name]; ok {
			installed[src+"/"+pkg.Version] = true
		}
	}

	var toRemove []string
	err = filepath.WalkDir(releaseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		// Path structure: releaseDir/github.com/<owner>/<repo>/<version>/<asset>
		rel, err := filepath.Rel(releaseDir, path)
		if err != nil {
			return nil
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) < 5 {
			return nil
		}
		// parts: [github.com, owner, repo, version, asset]
		source := "github.com/" + parts[1] + "/" + parts[2]
		ver := parts[3]
		if !installed[source+"/"+ver] {
			toRemove = append(toRemove, path)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if len(toRemove) == 0 {
		fmt.Println("Nothing to clean.")
		return nil
	}

	if dryRun {
		for _, p := range toRemove {
			fmt.Printf("[dry-run] would remove %s\n", p)
		}
		return nil
	}

	if !promptConfirm(fmt.Sprintf("Remove %d cached file(s)?", len(toRemove))) {
		fmt.Println("Aborted.")
		return nil
	}

	for _, p := range toRemove {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			color.Yellow("⚠ %s: %v", p, err)
		}
	}
	color.Green("✓ cleaned %d file(s)", len(toRemove))
	return nil
}
