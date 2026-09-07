package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/asset"
	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/store"
)

func newUpgradeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "upgrade",
		Aliases: []string{"ug", "upg"},
		Short:   "Upgrade ghpm to its latest release",
		Args:    cobra.NoArgs,
		RunE:    runUpgrade,
	}
	addSkipHashCheckFlag(cmd)
	return cmd
}

// runUpgrade upgrades ghpm and nothing else. The tools ghpm vendors for its
// own use (gh, sheesh's kebab) are deliberately not components here: they are
// pinned in internal/toolchain and synced to that pin at runtime, so "upgrade"
// means exactly what a user reading it expects — upgrade ghpm — rather than
// asking about internals they never chose to install.
func runUpgrade(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	ci, err := initCommand(ctx, cmdOptions{Lock: true, GH: true, SkipHashCheck: true})
	if err != nil {
		return err
	}
	defer ci.close()
	cfg := ci.cfg

	item, err := checkSelf(ctx, cfg, ci.gh)
	if err != nil {
		printFail(cfg, "%s: %v", binGhpm, err)
		printFailedTable(cfg, "component", []failedItem{{name: binGhpm, reason: err.Error()}})
		return errSilent
	}
	if item == nil {
		print(msgGhpmUpToDate)
		return nil
	}

	if !gate(
		[]string{"name", "version", "update"},
		[][]string{{item.name, item.current, item.latest}},
		[]func(string) string{nil, colorfn(cfg, "old"), colorfn(cfg, "new")},
		fmt.Sprintf("upgrade %s to %s", item.name, item.latest),
	) {
		return nil
	}

	if err := item.install(); err != nil {
		printFail(cfg, "%s: %v", item.name, err)
		printFailedTable(cfg, "component", []failedItem{{name: item.name, reason: err.Error()}})
		return errSilent
	}
	printPass(cfg, "upgraded %s to %s", item.name, item.latest)
	return nil
}

// upgradeItem is ghpm's own outdated release: the versions for the gate table
// and a closure that performs the actual install. The install is kept as a
// closure so runUpgrade's orchestration is testable without ever replacing
// the running binary.
type upgradeItem struct {
	name    string
	current string
	latest  string
	install func() error
}

func checkSelf(ctx context.Context, cfg *config.Settings, ghClient gh.Client) (*upgradeItem, error) {
	rel, err := ghClient.GetLatestRelease(ctx, config.RepoGhpm.Owner, config.RepoGhpm.Repo)
	if err != nil {
		return nil, err
	}
	latestVer := config.NormalizeVersion(rel.TagName)

	if strings.TrimPrefix(rel.TagName, "v") == strings.TrimPrefix(version, "v") {
		return nil, nil
	}

	install := func() error {
		_, ghpmBin, cleanup, err := fetchBinary(ctx, cfg, ghClient, config.RepoGhpm, rel, binGhpm)
		if err != nil {
			return err
		}
		if cleanup == nil {
			return nil
		}
		defer cleanup()

		self, err := os.Executable()
		if err != nil {
			return err
		}
		self, err = filepath.EvalSymlinks(self)
		if err != nil {
			return err
		}

		tmp := self + ".new"
		if err := copyFile(ghpmBin, tmp); err != nil {
			return err
		}
		if err := os.Chmod(tmp, 0755); err != nil {
			return err
		}
		if err := replaceSelf(tmp, self); err != nil {
			return err
		}

		return nil
	}
	return &upgradeItem{name: binGhpm, current: version, latest: latestVer, install: install}, nil
}

// fetchSelected selects an asset for pkgName, downloads, verifies, and extracts
// it into a fresh temp dir. It returns the chosen asset, the temp dir, and a
// cleanup func. On ErrSkip it returns an empty asset name and a nil cleanup.
func fetchSelected(ctx context.Context, cfg *config.Settings, ghClient gh.Client, repo config.RepoRef, rel gh.Release, pkgName string) (gh.Asset, string, func(), error) {
	ac, err := asset.SelectAssetAuto(rel.Assets, cfg, "", pkgName)
	if err != nil {
		return gh.Asset{}, "", nil, err
	}
	chosen, err := asset.PromptFromCandidates(ac, pkgName)
	if errors.Is(err, asset.ErrSkip) {
		return gh.Asset{}, "", nil, nil
	}
	if err != nil {
		return gh.Asset{}, "", nil, err
	}

	if dryRun {
		return chosen, "", nil, nil
	}

	cacheDir, err := store.ReleaseDir(repo.URI, rel.TagName)
	if err != nil {
		return gh.Asset{}, "", nil, err
	}
	if err := downloadAsset(ctx, ghClient, repo.Owner, repo.Repo, rel.DownloadTag(), chosen.Name, cacheDir, pkgName); err != nil {
		return gh.Asset{}, "", nil, err
	}
	if !skipHashCheck && chosen.Digest != "" {
		assetPath := filepath.Join(cacheDir, chosen.Name)
		if err := verifyDigest(chosen.Digest, assetPath); err != nil {
			return gh.Asset{}, "", nil, fmt.Errorf("%s: %s: %w", pkgName, chosen.Name, err)
		}
	}
	tmpDir, err := os.MkdirTemp("", "ghpm-upgrade-*")
	if err != nil {
		return gh.Asset{}, "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }
	if err := asset.ExtractPackage(cacheDir, chosen.Name, tmpDir); err != nil {
		cleanup()
		return gh.Asset{}, "", nil, err
	}
	return chosen, tmpDir, cleanup, nil
}

// fetchBinary runs fetchSelected then locates the named binary in the extract.
// Returns (tmpDir, binPath, cleanup, error). A skipped or dry-run selection
// yields an empty binPath with a nil cleanup.
func fetchBinary(ctx context.Context, cfg *config.Settings, ghClient gh.Client, repo config.RepoRef, rel gh.Release, pkgName string) (string, string, func(), error) {
	chosen, tmpDir, cleanup, err := fetchSelected(ctx, cfg, ghClient, repo, rel, pkgName)
	if err != nil || cleanup == nil {
		return "", "", cleanup, err
	}
	candidates := asset.FindBins(tmpDir)
	if len(candidates) == 0 {
		cleanup()
		return "", "", nil, fmt.Errorf(msgNoBinaryFound, chosen.Name)
	}
	binPath := filepath.Join(tmpDir, filepath.FromSlash(candidates[0].Key()))
	return tmpDir, binPath, cleanup, nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0755)
}

func winTempBinDir() string {
	return filepath.Join(os.TempDir(), "ghpm", "bin")
}

func replaceSelf(src, dst string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(src, dst)
	}
	tmpDir := winTempBinDir()
	_ = os.MkdirAll(tmpDir, 0755)
	old := filepath.Join(tmpDir, filepath.Base(dst))
	_ = os.Remove(old)
	if err := os.Rename(dst, old); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		_ = os.Rename(old, dst)
		return err
	}
	return nil
}
