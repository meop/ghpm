package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/shim"
)

func newRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <name> [name...]",
		Aliases: []string{"r", "rm", "rem", "un", "unin", "uninstall"},
		Short:   "Remove installed packages",
		Args:    cobra.MinimumNArgs(1),
		RunE:    runRemove,
	}
}

func runRemove(cmd *cobra.Command, args []string) error {
	ci, err := initCommand(context.Background(), cmdOptions{Lock: true, Manifest: true})
	if err != nil {
		return err
	}
	defer ci.close()
	cfg := ci.cfg
	manifest := ci.manifest
	pkgsDir, err := ci.dirs.ExtractsDir()
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
			print("%s: not installed", arg)
			continue
		}
		targets = append(targets, uninstallTarget{key: arg, pkg: pkg})
	}
	if len(targets) == 0 {
		return nil
	}

	var rows [][]string
	for _, t := range targets {
		baseName, _, _ := config.ParseVersionSuffix(t.key)
		repo := manifest.Repos[baseName]
		rows = append(rows, []string{t.key, t.pkg.Version, t.pkg.Pin, repo, strings.Join(t.pkg.Assets, ", ")})
	}
	colors := []func(string) string{nil, colorfn(cfg, "info"), nil, nil, nil}
	if !gate([]string{"name", "version", "pin", "uri", "assets"}, rows, colors, fmt.Sprintf("uninstall %d package(s)", len(targets))) {
		return nil
	}

	var hadErrors bool
	var failedItems []failedItem
	successCount := 0
	for _, t := range targets {
		pkgPath := filepath.Join(pkgsDir, t.key, t.pkg.Version)
		if err := os.RemoveAll(pkgPath); err != nil && !os.IsNotExist(err) {
			printFail(cfg, "%s: could not remove extract dir: %v", t.key, err)
			hadErrors = true
			failedItems = append(failedItems, failedItem{name: t.key, reason: fmt.Sprintf("could not remove extract dir: %v", err)})
			continue
		}
		baseDir := filepath.Join(pkgsDir, t.key)
		if entries, err := os.ReadDir(baseDir); err == nil && len(entries) == 0 {
			_ = os.Remove(baseDir)
		}
		manifest.RemoveExtract(t.key)
		for shimName := range t.pkg.AllBins() {
			if err := shim.Remove(shimName); err != nil {
				printWarn(cfg, "%s: could not remove shim: %v", shimName, err)
			}
		}
		if fontsDir, err := userFontDir(); err == nil {
			for fontName, fontPath := range t.pkg.AllFonts() {
				if err := uninstallFont(fontPath, fontsDir); err != nil {
					printWarn(cfg, "%s: could not remove font: %v", fontName, err)
				}
			}
		}
		successCount++
	}

	if successCount > 0 {
		printPass(cfg, "uninstalled %d package(s)", successCount)
	}
	printFailedTable(cfg, "package", failedItems)

	if err := saveManifest(cfg, manifest); err != nil {
		return err
	}

	if hadErrors {
		return errSilent
	}
	return nil
}
