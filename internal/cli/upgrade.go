package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
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
		Use:   "upgrade",
		Short: "Upgrade ghpm and gh to their latest releases",
		Args:  cobra.NoArgs,
		RunE:  runUpgrade,
	}
	addSkipHashCheckFlag(cmd)
	return cmd
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	ci, err := initCommand(cmdOptions{Lock: true, GH: true, SkipHashCheck: true})
	if err != nil {
		return err
	}
	defer ci.close()
	cfg := ci.cfg
	ctx := cmd.Context()
	ghClient := ci.gh

	// Phase 1: check each component's version (no install). Up-to-date ones are
	// silently dropped; outdated ones are collected for the gate. Like sync, the
	// no-op outcome is a single summary line (below), not one line per component.
	hadErrors := false
	var items []upgradeItem
	checks := []struct {
		name string
		fn   func(context.Context, *config.Settings, gh.Client) (*upgradeItem, error)
	}{
		{binGh, checkGh},
		{binGhpm, checkSelf},
		{binSheesh, checkShim},
	}
	for _, c := range checks {
		item, err := c.fn(ctx, cfg, ghClient)
		if err != nil {
			printFail(cfg, "%s: %v", c.name, err)
			hadErrors = true
			continue
		}
		if item != nil {
			items = append(items, *item)
		}
	}

	if len(items) == 0 {
		if hadErrors {
			return errSilent
		}
		print(msgAllComponentsUpToDate)
		return nil
	}

	// Gate: one table + one confirm for everything that will be upgraded.
	rows := make([][]string, 0, len(items))
	for _, it := range items {
		rows = append(rows, []string{it.name, it.current, it.latest})
	}
	if !gate([]string{"name", "version", "update"}, rows, []func(string) string{nil, colorfn(cfg, "old"), colorfn(cfg, "new")}, fmt.Sprintf("upgrade %d component(s)", len(items))) {
		return nil
	}

	// Phase 2: install each, prompting for assets only where ambiguous.
	for _, it := range items {
		if err := it.install(); err != nil {
			printFail(cfg, "%s: %v", it.name, err)
			hadErrors = true
		}
	}

	if hadErrors {
		return errSilent
	}
	return nil
}

// upgradeItem is one outdated self-managed component (gh, ghpm, sheesh): its
// versions for the gate table and a closure that performs the actual install.
type upgradeItem struct {
	name    string
	current string
	latest  string
	install func() error
}

// installedBinaryVersion runs `<path> --version` and returns the first version
// token found (without a leading "v"), or "" if the binary is absent or emits none.
func installedBinaryVersion(path string) string {
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return ""
	}
	for tok := range strings.FieldsSeq(string(out)) {
		if asset.IsVersionToken(tok) {
			return strings.TrimPrefix(tok, "v")
		}
	}
	return ""
}

func checkGh(ctx context.Context, cfg *config.Settings, ghClient gh.Client) (*upgradeItem, error) {
	binDir, err := store.BinDir()
	if err != nil {
		return nil, err
	}
	ghPath := filepath.Join(binDir, exeName(binGh))

	if _, err := os.Stat(ghPath); err != nil {
		return nil, nil
	}

	currentVer := installedBinaryVersion(ghPath)

	rel, err := ghClient.GetLatestRelease(ctx, config.RepoGh.Owner, config.RepoGh.Repo)
	if err != nil {
		return nil, err
	}
	latestVer := config.NormalizeVersion(rel.TagName)

	if currentVer == latestVer {
		return nil, nil
	}

	install := func() error {
		_, ghBin, cleanup, err := fetchBinary(ctx, cfg, ghClient, config.RepoGh, rel, binGh)
		if err != nil {
			return err
		}
		if cleanup == nil {
			return nil
		}
		defer cleanup()

		if err := os.MkdirAll(binDir, 0755); err != nil {
			return err
		}
		// See installFont: FILE_SHARE_DELETE lets Remove succeed on the running binary.
		_ = os.Remove(ghPath)
		if err := copyFile(ghBin, ghPath); err != nil {
			return err
		}
		if err := os.Chmod(ghPath, 0755); err != nil {
			return err
		}

		printPass(cfg, "%s: upgraded %s → %s", binGh, currentVer, latestVer)
		return nil
	}
	return &upgradeItem{name: binGh, current: currentVer, latest: latestVer, install: install}, nil
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

		printPass(cfg, "%s: upgraded %s → %s", binGhpm, version, latestVer)
		return nil
	}
	return &upgradeItem{name: binGhpm, current: version, latest: latestVer, install: install}, nil
}

func checkShim(ctx context.Context, cfg *config.Settings, ghClient gh.Client) (*upgradeItem, error) {
	shimDir, err := store.ShimDir()
	if err != nil {
		return nil, err
	}
	kebabPath := filepath.Join(shimDir, exeName("kebab"))

	currentVer := ""
	if _, err := os.Stat(kebabPath); err == nil {
		currentVer = installedBinaryVersion(kebabPath)
	}

	rel, err := ghClient.GetLatestRelease(ctx, config.RepoSheesh.Owner, config.RepoSheesh.Repo)
	if err != nil {
		return nil, err
	}
	latestVer := config.NormalizeVersion(rel.TagName)

	if currentVer == latestVer {
		return nil, nil
	}

	install := func() error {
		_, tmpDir, cleanup, err := fetchSelected(ctx, cfg, ghClient, config.RepoSheesh, rel, binSheesh)
		if err != nil {
			return err
		}
		if cleanup == nil {
			return nil
		}
		defer cleanup()

		if err := copyExecutablesToDir(tmpDir, shimDir); err != nil {
			return err
		}

		if currentVer == "" {
			printPass(cfg, "%s: installed %s", binSheesh, latestVer)
		} else {
			printPass(cfg, "%s: upgraded %s → %s", binSheesh, currentVer, latestVer)
		}
		return nil
	}
	return &upgradeItem{name: binSheesh, current: currentVer, latest: latestVer, install: install}, nil
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
	if ac.Chosen.Name != "" {
		print("%s: found asset [%s]", pkgName, chosen.Name)
	}

	if dryRun {
		return chosen, "", nil, nil
	}

	cacheDir, err := store.ReleaseDir(repo.URI, rel.TagName)
	if err != nil {
		return gh.Asset{}, "", nil, err
	}
	if err := ghClient.DownloadAsset(ctx, repo.Owner, repo.Repo, rel.TagName, chosen.Name, cacheDir); err != nil {
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

// copyExecutablesToDir walks srcDir recursively and copies all executable files
// (Unix: executable bit set; Windows: .exe suffix) to destDir flat.
func copyExecutablesToDir(srcDir, destDir string) error {
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		if runtime.GOOS == "windows" {
			if !strings.HasSuffix(strings.ToLower(name), ".exe") {
				return nil
			}
		} else {
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.Mode()&0111 == 0 {
				return nil
			}
		}
		dest := filepath.Join(destDir, name)
		_ = os.Remove(dest)
		if err := copyFile(path, dest); err != nil {
			return err
		}
		return os.Chmod(dest, 0755)
	})
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
