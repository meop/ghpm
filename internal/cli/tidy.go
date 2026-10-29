package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/shim"
	"github.com/meop/ghpm/internal/store"
	"github.com/meop/golie/tidy"
)

func newTidyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "tidy",
		Aliases: []string{"t", "ti", "cl", "clean"},
		Short:   "Clean unused downloads and orphaned items",
		Args:    cobra.NoArgs,
		RunE:    runTidy,
	}
	cmd.Flags().Bool("all", false, "Remove all cached assets regardless of installation status")
	return cmd
}

func runTidy(cmd *cobra.Command, args []string) error {
	ci, err := initCommand(context.Background(), cmdOptions{Lock: true, Manifest: true})
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
			print("remove all cached assets in %s", releaseDir)
		} else {
			if !promptConfirm("remove all download(s)") {
				return nil
			}
			if err := os.RemoveAll(releaseDir); err != nil {
				printFail(cfg, "%v", err)
				return errSilent
			}
			printPass(cfg, "removed all cached assets")
			_ = os.MkdirAll(releaseDir, 0755)
		}
		return nil
	}

	b1 := cleanBrokenInstalls(cfg, manifest, releaseDir)
	b2 := cleanOrphanedBinShims(cfg, manifest)
	b3 := cleanOrphanedFonts(cfg, manifest, releaseDir)
	b4 := cleanOrphanedExtracts(cfg, manifest)
	b5 := cleanOrphanedReleases(cfg, releaseDir, manifest)
	if !b1 && !b2 && !b3 && !b4 && !b5 {
		print("nothing to tidy")
	}
	return nil
}

// pruneExtract and pruneDownloadVersion are golie's; see its tidy package.
func pruneExtract(path, pkgsDir string) error { return tidy.PruneExtract(path, pkgsDir) }

func pruneDownloadVersion(releaseDir, src, version string) {
	tidy.PruneDownloadVersion(releaseDir, src, version)
}

// dryRunBail prints the shared dry-run closing message and returns true (the
// "there was something to report" signal every tidy clean* function returns).
func dryRunBail() bool {
	sep()
	print(msgDryRun)
	return true
}

// tidyDirs is the part of ~/.ghpm golie's tidy looks at.
func tidyDirs() (tidy.Dirs, error) {
	l, err := store.Layout()
	if err != nil {
		return tidy.Dirs{}, err
	}
	return tidy.DirsOf(l)
}

// cleanBrokenInstalls removes manifest entries whose shim or extract is missing,
// and trims BinNames for entries where only some shims are missing. What is
// broken and how it is removed are golie's (tidy.FindBroken, tidy.RemoveBroken).
func cleanBrokenInstalls(cfg *config.Settings, manifest *config.Manifest, releaseDir string) bool {
	dirs, err := tidyDirs()
	if err != nil {
		return false
	}
	dirs.Downloads = releaseDir

	items := tidy.FindBroken(manifest, dirs)
	if len(items) == 0 {
		return false
	}

	rows := make([][]string, len(items))
	for i, it := range items {
		rows[i] = []string{it.Key, it.Issue}
	}
	printTable([]string{"name", "issue"}, rows, nil)

	if dryRun {
		return dryRunBail()
	}

	if !promptConfirm(fmt.Sprintf("remove %d broken install(s)", len(items))) {
		return true
	}

	manifestTouched := false
	for _, it := range items {
		// A shim that will not delete keeps its manifest entry: dropping the
		// entry anyway would turn a file ghpm still owns into an orphan only
		// the *next* tidy could name, and only for bins — the font sweep does
		// not scan for untracked files at all.
		changed, stuck := tidy.RemoveBroken(manifest, it, dirs)
		for _, st := range stuck {
			printFail(cfg, "%s: could not remove shim: %v", filepath.Base(st.Path), st.Err)
		}
		manifestTouched = manifestTouched || changed
	}

	if manifestTouched {
		if err := saveManifest(cfg, manifest); err != nil {
			return true
		}
	}
	printPass(cfg, "removed %d broken install(s)", len(items))
	return true
}

// cleanOrphanedFonts removes manifest font entries that are not (fully) installed in the
// user font directory or registry. Because the font is tracked in the manifest, ghpm owns it
// and may also clean up any partial remnants (file or registry entry) left behind.
func cleanOrphanedFonts(cfg *config.Settings, manifest *config.Manifest, releaseDir string) bool {
	fontsDir, err := userFontDir()
	if err != nil {
		return false
	}
	pkgsDir, err := store.ExtractsDir()
	if err != nil {
		return false
	}

	type fontItem struct {
		tableRow    []string
		manifestKey string
		fontName    string
		fontFile    string
	}
	var items []fontItem

	for key, pkg := range manifest.Extracts {
		for fontName, fontPath := range pkg.Font {
			if !fontInstalled(fontPath, fontsDir) {
				items = append(items, fontItem{
					tableRow:    []string{key, fontName},
					manifestKey: key,
					fontName:    fontName,
					fontFile:    filepath.Base(fontPath),
				})
			}
		}
	}

	if len(items) == 0 {
		return false
	}

	slices.SortFunc(items, func(a, b fontItem) int {
		if c := strings.Compare(a.tableRow[0], b.tableRow[0]); c != 0 {
			return c
		}
		return strings.Compare(a.tableRow[1], b.tableRow[1])
	})
	fontRows := make([][]string, len(items))
	for i, it := range items {
		fontRows[i] = it.tableRow
	}
	printTable([]string{"package", "font"}, fontRows, nil)

	if dryRun {
		return dryRunBail()
	}

	if !promptConfirm(fmt.Sprintf("remove %d orphaned font(s)", len(items))) {
		return true
	}

	manifestTouched := false
	for _, it := range items {
		if err := os.Remove(filepath.Join(fontsDir, it.fontFile)); err != nil && !os.IsNotExist(err) {
			printFail(cfg, "%s: could not remove font: %v", it.fontFile, err)
			continue
		}
		unregisterFont(it.fontFile)

		entry, ok := manifest.Extracts[it.manifestKey]
		if !ok {
			continue
		}
		delete(entry.Font, it.fontName)
		manifest.Extracts[it.manifestKey] = entry
		if len(entry.AllBins()) == 0 && len(entry.AllFonts()) == 0 {
			baseName, _, _ := config.ParseVersionSuffix(it.manifestKey)
			if src, ok := manifest.Repos[baseName]; ok {
				pruneDownloadVersion(releaseDir, src, entry.Version)
			}
			manifest.RemoveExtract(it.manifestKey)
			_ = pruneExtract(filepath.Join(pkgsDir, it.manifestKey), pkgsDir)
		}
		manifestTouched = true
	}

	if manifestTouched {
		if err := saveManifest(cfg, manifest); err != nil {
			return true
		}
	}
	printPass(cfg, "removed %d orphaned font(s)", len(items))
	return true
}

// cleanOrphanedBinShims removes files in bin/ that have no corresponding manifest entry.
func cleanOrphanedBinShims(cfg *config.Settings, manifest *config.Manifest) bool {
	binDir, err := store.BinDir()
	if err != nil {
		return false
	}

	// only ghpm's own shims are ghpm's to clean up
	paths := tidy.FindOrphanShims(manifest, binDir, []string{exeName(binGhpm)}, shim.IsShim)
	binRows := make([][]string, len(paths))
	for i, p := range paths {
		binRows[i] = []string{filepath.Base(p)}
	}

	if len(paths) == 0 {
		return false
	}

	printTable([]string{"name"}, binRows, nil)

	if dryRun {
		return dryRunBail()
	}

	if !promptConfirm(fmt.Sprintf("remove %d orphaned bin(s)", len(paths))) {
		return true
	}

	removed := 0
	for _, p := range paths {
		if err := os.Remove(p); err != nil {
			printFail(cfg, "%s: %v", p, err)
			continue
		}
		removed++
	}
	if removed > 0 {
		printPass(cfg, "removed %d orphaned bin(s)", removed)
	}
	return true
}

// orphanState says why an extract dir is an orphan: no install names it, or an op left it staged or aside.
func orphanState(o tidy.OrphanExtract) string {
	if o.Leftover {
		return "left over"
	}
	return "not installed"
}

// cleanOrphanedExtracts removes extract dirs no manifest entry names, and what an op left staged or aside.
func cleanOrphanedExtracts(cfg *config.Settings, manifest *config.Manifest) bool {
	pkgsDir, err := store.ExtractsDir()
	if err != nil {
		return false
	}

	var paths []string
	var extRows [][]string
	for _, o := range tidy.FindOrphanExtracts(manifest, pkgsDir) {
		paths = append(paths, o.Path)
		extRows = append(extRows, []string{o.Key, orphanState(o)})
	}

	if len(paths) == 0 {
		return false
	}

	printTable([]string{"name", "state"}, extRows, nil)

	if dryRun {
		return dryRunBail()
	}

	if !promptConfirm(fmt.Sprintf("remove %d orphaned extract(s)", len(paths))) {
		return true
	}

	removed := 0
	for _, p := range paths {
		if err := pruneExtract(p, pkgsDir); err != nil {
			printFail(cfg, "%s: %v", p, err)
			continue
		}
		removed++
	}
	if removed > 0 {
		printPass(cfg, "removed %d orphaned extract(s)", removed)
	}
	return true
}

// sourceDepth is how many path elements a source has under download/:
// github.com/owner/repo.
const sourceDepth = 3

func cleanOrphanedReleases(cfg *config.Settings, releaseDir string, manifest *config.Manifest) bool {
	var toRemove []string
	var dlRows [][]string
	for _, o := range tidy.FindOrphanDownloads(manifest, releaseDir, sourceDepth) {
		toRemove = append(toRemove, o.Path)
		dlRows = append(dlRows, []string{o.Package(), o.Version, o.File})
	}

	if len(toRemove) == 0 {
		return false
	}

	printTable([]string{"package", "version", "file"}, dlRows, nil)

	if dryRun {
		return dryRunBail()
	}

	if !promptConfirm(fmt.Sprintf("remove %d orphaned download(s)", len(toRemove))) {
		return true
	}

	removed := 0
	for _, p := range toRemove {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			printFail(cfg, "%s: %v", p, err)
			continue
		}
		removed++
	}
	tidy.PruneEmptyDirs(releaseDir)
	if removed > 0 {
		printPass(cfg, "removed %d orphaned download(s)", removed)
	}
	return true
}
