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
	addSkipVerifyFlag(cmd)
	return cmd
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	ci, err := initCommand(cmdOptions{Lock: true, GH: true, NoVerify: true})
	if err != nil {
		return err
	}
	defer ci.close()
	cfg := ci.cfg

	hadErrors := false
	ctx := cmd.Context()
	ghClient := ci.gh

	if err := upgradeGh(ctx, cfg, ghClient); err != nil {
		printFail(cfg, "gh: %v", err)
		hadErrors = true
	}
	if err := upgradeSelf(ctx, cfg, ghClient); err != nil {
		printFail(cfg, "ghpm: %v", err)
		hadErrors = true
	}
	if err := upgradeShim(ctx, cfg, ghClient); err != nil {
		printFail(cfg, "sheesh: %v", err)
		hadErrors = true
	}

	if hadErrors {
		return errSilent
	}
	return nil
}

func upgradeGh(ctx context.Context, cfg *config.Settings, ghClient gh.Client) error {
	binDir, err := store.BinDir()
	if err != nil {
		return err
	}
	ghPath := filepath.Join(binDir, exeName(binGh))

	if _, err := os.Stat(ghPath); err != nil {
		return nil
	}

	currentVer := ""
	if out, err := exec.Command(ghPath, "--version").Output(); err == nil {
		for _, tok := range strings.Fields(string(out)) {
			if asset.IsVersionToken(tok) {
				currentVer = strings.TrimPrefix(tok, "v")
				break
			}
		}
	}

	rel, err := ghClient.GetLatestRelease(ctx, config.RepoGh.Owner, config.RepoGh.Repo)
	if err != nil {
		return err
	}
	latestVer := config.NormalizeVersion(rel.TagName)

	if currentVer == latestVer {
		fmt.Printf("gh: already latest → %s\n", currentVer)
		return nil
	}

	if !promptConfirm(fmt.Sprintf("gh: upgrade %s → %s", currentVer, latestVer)) {
		return nil
	}

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

	printPass(cfg, "gh: upgraded %s → %s", currentVer, latestVer)
	sep()
	return nil
}

func upgradeSelf(ctx context.Context, cfg *config.Settings, ghClient gh.Client) error {
	rel, err := ghClient.GetLatestRelease(ctx, config.RepoGhpm.Owner, config.RepoGhpm.Repo)
	if err != nil {
		return err
	}
	latestVer := config.NormalizeVersion(rel.TagName)

	if strings.TrimPrefix(rel.TagName, "v") == strings.TrimPrefix(version, "v") {
		fmt.Printf("ghpm: already latest → %s\n", version)
		return nil
	}

	if !promptConfirm(fmt.Sprintf("ghpm: upgrade %s → %s", version, latestVer)) {
		return nil
	}

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

	printPass(cfg, "ghpm: upgraded %s → %s", version, latestVer)
	sep()
	return nil
}

func upgradeShim(ctx context.Context, cfg *config.Settings, ghClient gh.Client) error {
	shimDir, err := store.ShimDir()
	if err != nil {
		return err
	}
	kebabPath := filepath.Join(shimDir, exeName("kebab"))

	currentVer := ""
	if _, err := os.Stat(kebabPath); err == nil {
		if out, err := exec.Command(kebabPath, "--version").Output(); err == nil {
			for _, tok := range strings.Fields(string(out)) {
				if asset.IsVersionToken(tok) {
					currentVer = strings.TrimPrefix(tok, "v")
					break
				}
			}
		}
	}

	rel, err := ghClient.GetLatestRelease(ctx, config.RepoSheesh.Owner, config.RepoSheesh.Repo)
	if err != nil {
		return err
	}
	latestVer := config.NormalizeVersion(rel.TagName)

	if currentVer == latestVer {
		fmt.Printf("sheesh: already latest → %s\n", currentVer)
		return nil
	}

	var action string
	if currentVer == "" {
		action = fmt.Sprintf("install %s", latestVer)
	} else {
		action = fmt.Sprintf("upgrade %s → %s", currentVer, latestVer)
	}

	if !promptConfirm("sheesh: " + action) {
		return nil
	}

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
		printPass(cfg, "sheesh: installed %s", latestVer)
	} else {
		printPass(cfg, "sheesh: upgraded %s → %s", currentVer, latestVer)
	}
	return nil
}

// fetchSelected selects an asset for pkgName, downloads, verifies, and extracts
// it into a fresh temp dir. It returns the chosen asset, the temp dir, and a
// cleanup func. On ErrSkip it returns an empty asset name and a nil cleanup.
func fetchSelected(ctx context.Context, cfg *config.Settings, ghClient gh.Client, repo config.RepoRef, rel gh.Release, pkgName string) (gh.Asset, string, func(), error) {
	ac, err := asset.SelectAssetAuto(rel.Assets, cfg, "", pkgName)
	if err != nil {
		return gh.Asset{}, "", nil, err
	}
	chosen, err := asset.PromptFromCandidates(ac)
	if errors.Is(err, asset.ErrSkip) {
		return gh.Asset{}, "", nil, nil
	}
	if err != nil {
		return gh.Asset{}, "", nil, err
	}
	if ac.Chosen.Name != "" {
		printInfo(cfg, "asset: %s", chosen.Name)
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
	if !noVerify {
		if verified, _ := ghClient.VerifyAsset(ctx, repo.Owner, repo.Repo, rel.TagName, cacheDir, chosen.Name); !verified {
			printWarn(cfg, "%s: %s unverified (no attestation)", pkgName, chosen.Name)
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
	candidates := asset.FindBins(tmpDir, pkgName)
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
