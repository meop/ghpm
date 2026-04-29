package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/env"
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
	manifest, err := config.LoadManifest()
	if err != nil {
		printFail(cfg, "could not load manifest: %v", err)
		return errSilent
	}
	pkgsDir, err := store.ExtractsDir()
	if err != nil {
		printFail(cfg, "%v", err)
		return errSilent
	}

	type uninstallTarget struct {
		key string
		pkg config.PackageEntry
	}

	var targets []uninstallTarget
	for _, arg := range args {
		pkg, ok := manifest.Extracts[arg]
		if !ok {
			printInfo(cfg, "%s: not installed", arg)
			continue
		}
		targets = append(targets, uninstallTarget{key: arg, pkg: pkg})
	}
	if len(targets) == 0 {
		return nil
	}

	if dryRun {
		for _, t := range targets {
			fmt.Printf("[dry-run] would remove %s %s (extract dir: %s)\n", t.key, t.pkg.Version, filepath.Join(pkgsDir, t.key, t.pkg.Version))
		}
		return nil
	}

	rows := make([][]string, len(targets))
	for i, t := range targets {
		baseName, _, _ := config.ParseVersionSuffix(t.key)
		rows[i] = []string{t.key, t.pkg.Pin, t.pkg.Version, t.pkg.AssetName, manifest.Repos[baseName]}
	}
	colors := []func(string) string{nil, nil, colorfn(cfg, "info"), nil, nil}
	printTable([]string{"name", "pin", "version", "asset", "repo"}, rows, colors)
	fmt.Println()
	if !promptConfirm(fmt.Sprintf("uninstall %d package(s)", len(targets))) {
		fmt.Println("aborted")
		return nil
	}

	var hadErrors bool
	for _, t := range targets {
		pkgPath := filepath.Join(pkgsDir, t.key, t.pkg.Version)
		if err := os.RemoveAll(pkgPath); err != nil && !os.IsNotExist(err) {
			printFail(cfg, "%s: could not remove extract dir: %v", t.key, err)
			hadErrors = true
			continue
		}
		// Remove base dir if now empty (no other version dirs remain)
		baseDir := filepath.Join(pkgsDir, t.key)
		if entries, err := os.ReadDir(baseDir); err == nil && len(entries) == 0 {
			_ = os.Remove(baseDir)
		}
		delete(manifest.Extracts, t.key)
		baseName, _, _ := config.ParseVersionSuffix(t.key)
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
		printPass(cfg, "uninstalled %s", t.key)
	}

	if err := config.SaveManifest(manifest); err != nil {
		printFail(cfg, "could not save manifest: %v", err)
		return errSilent
	}

	if _, err := env.Generate(manifest); err != nil {
		printWarn(cfg, "could not generate env files: %v", err)
	}

	if hadErrors {
		return errSilent
	}
	return nil
}
