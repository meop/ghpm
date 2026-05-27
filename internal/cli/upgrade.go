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
	cmd.Flags().BoolVarP(&noVerify, "skip-verify", "s", false, "Skip SHA256 verification")
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

	if err := upgradeGh(ctx, cfg); err != nil {
		printFail(cfg, "gh: %v", err)
		hadErrors = true
	}
	if err := upgradeSelf(ctx, cfg); err != nil {
		printFail(cfg, "ghpm: %v", err)
		hadErrors = true
	}
	if err := upgradeShim(ctx, cfg); err != nil {
		printFail(cfg, "sheesh: %v", err)
		hadErrors = true
	}

	if hadErrors {
		return errSilent
	}
	return nil
}

func upgradeGh(ctx context.Context, cfg *config.Settings) error {
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
		for _, tok := range asset.Tokenize(strings.TrimSpace(string(out))) {
			if asset.IsVersionToken(tok) {
				currentVer = strings.TrimPrefix(tok, "v")
				break
			}
		}
	}

	rel, err := gh.GetLatestRelease(ctx, config.RepoGh.Owner, config.RepoGh.Repo)
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

	acGh, err := asset.SelectAssetAuto(rel.Assets, cfg, "", binGh)
	if err != nil {
		return err
	}
	chosen, err := asset.PromptFromCandidates(acGh)
	if errors.Is(err, asset.ErrSkip) {
		return nil
	}
	if err != nil {
		return err
	}
	if acGh.Chosen.Name != "" {
		printInfo(cfg, "asset: %s", chosen.Name)
	}

	if dryRun {
		fmt.Printf("gh: upgrade %s → %s (asset: %s)\n", currentVer, latestVer, chosen.Name)
		return nil
	}

	cacheDir, err := store.ReleaseDir(config.RepoGh.URI, rel.TagName)
	if err != nil {
		return err
	}
	if err := gh.DownloadAsset(ctx, config.RepoGh.Owner, config.RepoGh.Repo, rel.TagName, chosen.Name, cacheDir); err != nil {
		return err
	}
	if !noVerify {
		_, _ = gh.VerifyAsset(ctx, config.RepoGh.Owner, config.RepoGh.Repo, rel.TagName, cacheDir, chosen.Name)
	}

	tmpDir, err := os.MkdirTemp("", "ghpm-gh-upgrade-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if err := asset.ExtractPackage(cacheDir, chosen.Name, tmpDir); err != nil {
		return err
	}
	ghCandidates := asset.FindBinaries(tmpDir, binGh)
	if len(ghCandidates) == 0 {
		return fmt.Errorf("no binary found in %s", chosen.Name)
	}
	ghBin := filepath.Join(tmpDir, filepath.FromSlash(ghCandidates[0].Key()))

	if err := os.MkdirAll(binDir, 0755); err != nil {
		return err
	}
	if err := replaceFile(ghBin, ghPath); err != nil {
		return err
	}
	if err := os.Chmod(ghPath, 0755); err != nil {
		return err
	}

	printPass(cfg, "gh: upgraded %s → %s", currentVer, latestVer)
	sep()
	return nil
}

func upgradeSelf(ctx context.Context, cfg *config.Settings) error {
	rel, err := gh.GetLatestRelease(ctx, config.RepoGhpm.Owner, config.RepoGhpm.Repo)
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

	acGhpm, err := asset.SelectAssetAuto(rel.Assets, cfg, "", binGhpm)
	if err != nil {
		return err
	}
	chosen, err := asset.PromptFromCandidates(acGhpm)
	if errors.Is(err, asset.ErrSkip) {
		return nil
	}
	if err != nil {
		return err
	}
	if acGhpm.Chosen.Name != "" {
		printInfo(cfg, "asset: %s", chosen.Name)
	}

	if dryRun {
		fmt.Printf("ghpm: upgrade %s → %s (asset: %s)\n", version, latestVer, chosen.Name)
		return nil
	}

	cacheDir, err := store.ReleaseDir(config.RepoGhpm.URI, rel.TagName)
	if err != nil {
		return err
	}
	if err := gh.DownloadAsset(ctx, config.RepoGhpm.Owner, config.RepoGhpm.Repo, rel.TagName, chosen.Name, cacheDir); err != nil {
		return err
	}
	if !noVerify {
		_, _ = gh.VerifyAsset(ctx, config.RepoGhpm.Owner, config.RepoGhpm.Repo, rel.TagName, cacheDir, chosen.Name)
	}

	tmpDir, err := os.MkdirTemp("", "ghpm-upgrade-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if err := asset.ExtractPackage(cacheDir, chosen.Name, tmpDir); err != nil {
		return err
	}
	ghpmCandidates := asset.FindBinaries(tmpDir, binGhpm)
	if len(ghpmCandidates) == 0 {
		return fmt.Errorf("no binary found in %s", chosen.Name)
	}
	ghpmBin := filepath.Join(tmpDir, filepath.FromSlash(ghpmCandidates[0].Key()))

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

func upgradeShim(ctx context.Context, cfg *config.Settings) error {
	shimDir, err := store.ShimDir()
	if err != nil {
		return err
	}
	kebabPath := filepath.Join(shimDir, exeName("kebab"))

	currentVer := ""
	if _, err := os.Stat(kebabPath); err == nil {
		if out, err := exec.Command(kebabPath, "--version").Output(); err == nil {
			currentVer = strings.TrimSpace(string(out))
		}
	}

	rel, err := gh.GetLatestRelease(ctx, config.RepoSheesh.Owner, config.RepoSheesh.Repo)
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

	acSheesh, err := asset.SelectAssetAuto(rel.Assets, cfg, "", binSheesh)
	if err != nil {
		return err
	}
	chosen, err := asset.PromptFromCandidates(acSheesh)
	if errors.Is(err, asset.ErrSkip) {
		return nil
	}
	if err != nil {
		return err
	}

	if dryRun {
		fmt.Printf("sheesh: %s (asset: %s)\n", action, chosen.Name)
		return nil
	}

	cacheDir, err := store.ReleaseDir(config.RepoSheesh.URI, rel.TagName)
	if err != nil {
		return err
	}
	if err := gh.DownloadAsset(ctx, config.RepoSheesh.Owner, config.RepoSheesh.Repo, rel.TagName, chosen.Name, cacheDir); err != nil {
		return err
	}
	if !noVerify {
		_, _ = gh.VerifyAsset(ctx, config.RepoSheesh.Owner, config.RepoSheesh.Repo, rel.TagName, cacheDir, chosen.Name)
	}

	tmpDir, err := os.MkdirTemp("", "ghpm-shim-upgrade-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if err := asset.ExtractPackage(cacheDir, chosen.Name, tmpDir); err != nil {
		return err
	}
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
		if err := replaceFile(path, dest); err != nil {
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

// replaceFile copies src to dst. On Windows, the existing dst is first moved to
// the shared temp staging dir so that a locked (in-use) executable can be displaced.
func replaceFile(src, dst string) error {
	if runtime.GOOS == "windows" {
		tmpDir := winTempBinDir()
		_ = os.MkdirAll(tmpDir, 0755)
		old := filepath.Join(tmpDir, filepath.Base(dst))
		_ = os.Remove(old)
		_ = os.Rename(dst, old)
	}
	return copyFile(src, dst)
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
