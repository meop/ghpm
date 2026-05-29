package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/shim"
	"github.com/meop/ghpm/internal/store"
)

func newRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <name> [name...]",
		Aliases: []string{"rm", "rem", "un", "unin", "uninstall"},
		Short:   "Remove installed packages",
		Args:    cobra.MinimumNArgs(1),
		RunE:    runRemove,
	}
}

func runRemove(cmd *cobra.Command, args []string) error {
	ci, err := initCommand(cmdOptions{Lock: true, Manifest: true})
	if err != nil {
		return err
	}
	defer ci.close()
	cfg := ci.cfg
	manifest := ci.manifest
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
			fmt.Printf("%s: remove %s (extract dir: %s)\n", t.key, t.pkg.Version, filepath.Join(pkgsDir, t.key, t.pkg.Version))
		}
		return nil
	}

	var rows [][]string
	for _, t := range targets {
		baseName, _, _ := config.ParseVersionSuffix(t.key)
		repo := manifest.Repos[baseName]
		assetNames := make([]string, 0, len(t.pkg.Asset))
		for assetName := range t.pkg.Asset {
			assetNames = append(assetNames, assetName)
		}
		slices.Sort(assetNames)
		for _, assetName := range assetNames {
			rows = append(rows, []string{t.key, t.pkg.Pin, t.pkg.Version, assetName, repo})
		}
	}
	colors := []func(string) string{nil, nil, colorfn(cfg, "info"), nil, nil}
	printTable([]string{"name", "pin", "version", "asset", "repo"}, rows, colors)
	sep()
	if !promptConfirm(fmt.Sprintf("uninstall %d package(s)", len(targets))) {
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
		for shimName := range t.pkg.AllBins() {
			if err := shim.Remove(shimName); err != nil {
				printWarn(cfg, "%s: could not remove shim: %v", shimName, err)
			}
		}
		if fontsDir, err := userFontDir(); err == nil {
			for _, fontPath := range t.pkg.AllFonts() {
				uninstallFont(fontPath, fontsDir)
			}
		}
		printPass(cfg, "%s: uninstalled", t.key)
	}

	if err := config.SaveManifest(manifest); err != nil {
		printFail(cfg, "could not save manifest: %v", err)
		return errSilent
	}

	if hadErrors {
		return errSilent
	}
	return nil
}
