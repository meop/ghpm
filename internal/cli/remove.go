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
	// Shims and fonts first, extract dir last, manifest entry last of all: every
	// step here destroys something, so nothing can be rolled back — what keeps
	// the state consistent instead is ordering plus a strict commit. A shim that
	// will not delete (a binary the running shell holds open) leaves the package
	// fully described in the manifest, extract dir and all, so the retry that
	// finishes the job has everything it needs. Dropping the entry there would
	// strand the shim as an orphan only tidy could name.
	for _, t := range targets {
		failReason := ""
		for shimName := range t.pkg.AllBins() {
			if err := shim.Remove(shimName); err != nil {
				printFail(cfg, "%s: %s: could not remove shim: %v", t.key, shimName, err)
				if failReason == "" {
					failReason = fmt.Sprintf("%s: could not remove shim: %v", shimName, err)
				}
			}
		}
		if fontsDir, err := userFontDir(); err == nil {
			for fontName, fontPath := range t.pkg.AllFonts() {
				if err := uninstallFont(fontPath, fontsDir); err != nil {
					printFail(cfg, "%s: %s: could not remove font: %v", t.key, fontName, err)
					if failReason == "" {
						failReason = fmt.Sprintf("%s: could not remove font: %v", fontName, err)
					}
				}
			}
		}
		if failReason == "" {
			pkgPath := filepath.Join(pkgsDir, t.key, t.pkg.Version)
			if err := os.RemoveAll(pkgPath); err != nil && !os.IsNotExist(err) {
				failReason = fmt.Sprintf("could not remove extract dir: %v", err)
				printFail(cfg, "%s: %s", t.key, failReason)
			}
		}
		if failReason != "" {
			hadErrors = true
			failedItems = append(failedItems, failedItem{name: t.key, reason: failReason})
			continue
		}
		baseDir := filepath.Join(pkgsDir, t.key)
		if entries, err := os.ReadDir(baseDir); err == nil && len(entries) == 0 {
			_ = os.Remove(baseDir)
		}
		manifest.RemoveExtract(t.key)
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
