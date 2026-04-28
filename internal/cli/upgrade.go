package cli

import (
	"fmt"
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
	return &cobra.Command{
		Use:   "upgrade",
		Short: "upgrade ghpm and gh to their latest releases",
		Args:  cobra.NoArgs,
		RunE:  runUpgrade,
	}
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	unlock, err := config.AcquireLock()
	if err != nil {
		printFail(nil, "%v", err)
		return errSilent
	}
	defer unlock()

	if err := gh.CheckInstalled(); err != nil {
		printFail(nil, "%v", err)
		return errSilent
	}
	cfg, err := config.LoadSettings()
	if err != nil {
		printFail(nil, "could not load settings: %v", err)
		return errSilent
	}
	if cfg.NoVerify {
		noVerify = true
	}

	upgraded := false

	if err := upgradeSelf(cfg); err != nil {
		printFail(cfg, "ghpm: %v", err)
		upgraded = true
	}
	if err := upgradeGh(cfg); err != nil {
		printFail(cfg, "gh: %v", err)
		upgraded = true
	}

	if upgraded {
		return errSilent
	}
	return nil
}

func upgradeSelf(cfg *config.Settings) error {
	rel, err := gh.GetLatestRelease("meop", "ghpm")
	if err != nil {
		return err
	}

	if strings.TrimPrefix(rel.TagName, "v") == strings.TrimPrefix(version, "v") {
		printInfo(cfg, "ghpm %s is already the latest version", version)
		return nil
	}

	chosen, err := asset.SelectAsset(rel.Assets, cfg, "")
	if err != nil {
		return err
	}

	if dryRun {
		fmt.Printf("[dry-run] would upgrade ghpm %s → %s (asset: %s)\n", version, config.NormalizeVersion(rel.TagName), chosen.Name)
		return nil
	}

	if !promptConfirm(fmt.Sprintf("upgrade ghpm %s → %s", version, config.NormalizeVersion(rel.TagName))) {
		fmt.Println("aborted")
		return nil
	}

	cacheDir, err := store.ReleaseDir("github.com/meop/ghpm", rel.TagName)
	if err != nil {
		return err
	}
	if err := gh.DownloadAsset("meop", "ghpm", rel.TagName, chosen.Name, cacheDir); err != nil {
		return err
	}
	if !noVerify {
		_, _ = asset.VerifySHA("meop", "ghpm", rel.TagName, cacheDir, chosen.Name, rel.Assets)
	}

	tmpDir, err := os.MkdirTemp("", "ghpm-upgrade-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if _, err := asset.Extract(cacheDir, chosen.Name, tmpDir, "ghpm", ""); err != nil {
		return err
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return err
	}

	tmp := self + ".new"
	if err := copyFile(filepath.Join(tmpDir, "ghpm"), tmp); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0755); err != nil {
		return err
	}
	if err := os.Rename(tmp, self); err != nil {
		return err
	}

	printPass(cfg, "upgraded ghpm %s → %s", version, config.NormalizeVersion(rel.TagName))
	return nil
}

func upgradeGh(cfg *config.Settings) error {
	binDir, err := store.BinDir()
	if err != nil {
		return err
	}
	ghPath := filepath.Join(binDir, "gh")
	if runtime.GOOS == "windows" {
		ghPath += ".exe"
	}

	currentVer := ""
	if info, err := os.Stat(ghPath); err == nil && info.Mode()&0111 != 0 {
		out, err := exec.Command(ghPath, "--version").Output()
		if err == nil {
			fields := strings.Fields(strings.TrimSpace(string(out)))
			for _, f := range fields {
				if strings.HasPrefix(f, "v") || (len(f) > 0 && f[0] >= '0' && f[0] <= '9') {
					currentVer = strings.TrimPrefix(f, "v")
					break
				}
			}
		}
	} else {
		return nil
	}

	rel, err := gh.GetLatestRelease("cli", "cli")
	if err != nil {
		return err
	}
	latestVer := config.NormalizeVersion(rel.TagName)

	if currentVer == latestVer {
		printInfo(cfg, "gh %s is already the latest version", currentVer)
		return nil
	}

	chosen, err := asset.SelectAsset(rel.Assets, cfg, "")
	if err != nil {
		return err
	}

	if dryRun {
		fmt.Printf("[dry-run] would upgrade gh %s → %s (asset: %s)\n", currentVer, latestVer, chosen.Name)
		return nil
	}

	if !promptConfirm(fmt.Sprintf("upgrade gh %s → %s", currentVer, latestVer)) {
		fmt.Println("aborted")
		return nil
	}

	cacheDir, err := store.ReleaseDir("github.com/cli/cli", rel.TagName)
	if err != nil {
		return err
	}
	if err := gh.DownloadAsset("cli", "cli", rel.TagName, chosen.Name, cacheDir); err != nil {
		return err
	}
	if !noVerify {
		_, _ = asset.VerifySHA("cli", "cli", rel.TagName, cacheDir, chosen.Name, rel.Assets)
	}

	tmpDir, err := os.MkdirTemp("", "ghpm-gh-upgrade-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if _, err := asset.Extract(cacheDir, chosen.Name, tmpDir, "gh", ""); err != nil {
		return err
	}

	binaryName := "gh"
	if runtime.GOOS == "windows" {
		binaryName = "gh.exe"
	}
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(tmpDir, binaryName), ghPath); err != nil {
		return err
	}
	if err := os.Chmod(ghPath, 0755); err != nil {
		return err
	}

	printPass(cfg, "upgraded gh %s → %s", currentVer, latestVer)
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0755)
}
