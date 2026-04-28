package cli

import (
	"fmt"
	"os"
	"path/filepath"
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
		Short: "upgrade ghpm itself to the latest release",
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

	rel, err := gh.GetLatestRelease("meop", "ghpm")
	if err != nil {
		printFail(cfg, "%v", err)
		return errSilent
	}

	if strings.TrimPrefix(rel.TagName, "v") == strings.TrimPrefix(version, "v") {
		printInfo(cfg, "ghpm %s is already the latest version", version)
		return nil
	}

	chosen, err := asset.SelectAsset(rel.Assets, cfg, "")
	if err != nil {
		printFail(cfg, "%v", err)
		return errSilent
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
		printFail(cfg, "%v", err)
		return errSilent
	}
	if err := gh.DownloadAsset("meop", "ghpm", rel.TagName, chosen.Name, cacheDir); err != nil {
		printFail(cfg, "%v", err)
		return errSilent
	}
	if !noVerify {
		verified, err := asset.VerifySHA("meop", "ghpm", rel.TagName, cacheDir, chosen.Name, rel.Assets)
		if err != nil {
			printFail(cfg, "SHA verification failed: %v", err)
			return errSilent
		}
		if !verified {
			printWarn(cfg, "no SHA256 checksum available, verification skipped")
		}
	}

	tmpDir, err := os.MkdirTemp("", "ghpm-upgrade-*")
	if err != nil {
		printFail(cfg, "%v", err)
		return errSilent
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if _, err := asset.Extract(cacheDir, chosen.Name, tmpDir, "ghpm", ""); err != nil {
		printFail(cfg, "%v", err)
		return errSilent
	}

	self, err := os.Executable()
	if err != nil {
		printFail(cfg, "%v", err)
		return errSilent
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		printFail(cfg, "%v", err)
		return errSilent
	}

	tmp := self + ".new"
	if err := copyFile(filepath.Join(tmpDir, "ghpm"), tmp); err != nil {
		printFail(cfg, "%v", err)
		return errSilent
	}
	if err := os.Chmod(tmp, 0755); err != nil {
		printFail(cfg, "%v", err)
		return errSilent
	}
	if err := os.Rename(tmp, self); err != nil {
		printFail(cfg, "%v", err)
		return errSilent
	}

	printPass(cfg, "upgraded ghpm %s → %s", version, config.NormalizeVersion(rel.TagName))
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0755)
}
