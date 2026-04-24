package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/store"
)

func newUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <name> [name...]",
		Short: "Remove installed packages",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runUninstall,
	}
}

func runUninstall(cmd *cobra.Command, args []string) error {
	manifest, err := config.LoadManifest()
	if err != nil {
		return err
	}
	binDir, err := store.BinDir()
	if err != nil {
		return err
	}

	type uninstallTarget struct {
		key string
		pkg config.PackageEntry
	}

	var targets []uninstallTarget
	for _, arg := range args {
		pkg, ok := manifest.Installs[arg]
		if !ok {
			color.Yellow("⚠ %s: not installed", arg)
			continue
		}
		targets = append(targets, uninstallTarget{key: arg, pkg: pkg})
	}
	if len(targets) == 0 {
		return nil
	}

	binNameFor := func(t uninstallTarget) string { return t.key }

	if dryRun {
		for _, t := range targets {
			fmt.Printf("[dry-run] would remove %s %s (binary: %s)\n", t.key, t.pkg.Version, filepath.Join(binDir, binNameFor(t)))
		}
		return nil
	}

	var msg string
	if len(targets) == 1 {
		msg = fmt.Sprintf("Uninstall %s %s?", targets[0].key, targets[0].pkg.Version)
	} else {
		msg = fmt.Sprintf("Uninstall %d packages?", len(targets))
	}
	if !promptConfirm(msg) {
		fmt.Println("Aborted.")
		return nil
	}

	for _, t := range targets {
		binPath := filepath.Join(binDir, binNameFor(t))
		if err := os.Remove(binPath); err != nil && !os.IsNotExist(err) {
			color.Yellow("⚠ %s: could not remove binary: %v", t.key, err)
			continue
		}
		delete(manifest.Installs, t.key)
		// Remove source entry if no packages with this base name remain
		baseName, _, _ := config.ParseVersionSuffix(t.key)
		hasOther := false
		for k := range manifest.Installs {
			if n, _, _ := config.ParseVersionSuffix(k); n == baseName {
				hasOther = true
				break
			}
		}
		if !hasOther {
			delete(manifest.Tools, baseName)
		}
		color.Green("✓ uninstalled %s", t.key)
	}

	return config.SaveManifest(manifest)
}
