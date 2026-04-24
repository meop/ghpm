package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/asset"
	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/store"
)

func newUpgradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade ghpm itself to the latest release",
		Args:  cobra.NoArgs,
		RunE:  runUpgrade,
	}
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	if err := gh.CheckInstalled(); err != nil {
		return err
	}
	cfg, err := config.LoadSettings()
	if err != nil {
		return err
	}
	if cfg.NoVerify {
		NoVerify = true
	}

	rel, err := gh.GetLatestRelease("meop", "ghpm")
	if err != nil {
		return err
	}

	if strings.TrimPrefix(rel.TagName, "v") == strings.TrimPrefix(version, "v") {
		fmt.Printf("ghpm %s is already the latest version.\n", version)
		return nil
	}

	chosen, err := asset.SelectAsset(rel.Assets, cfg, "")
	if err != nil {
		return err
	}

	if DryRun {
		fmt.Printf("[dry-run] would upgrade ghpm %s → %s (asset: %s)\n", version, rel.TagName, chosen.Name)
		return nil
	}

	if !promptConfirm(fmt.Sprintf("Upgrade ghpm %s → %s?", version, rel.TagName)) {
		fmt.Println("Aborted.")
		return nil
	}

	cacheDir, err := store.ReleaseDir("github.com/meop/ghpm", rel.TagName)
	if err != nil {
		return err
	}
	if err := gh.DownloadAsset("meop", "ghpm", rel.TagName, chosen.Name, cacheDir); err != nil {
		return err
	}
	if !NoVerify {
		if err := asset.VerifySHA("meop", "ghpm", rel.TagName, cacheDir, chosen.Name, rel.Assets); err != nil {
			return fmt.Errorf("SHA verification failed: %w", err)
		}
	}

	// Extract to a temp location then replace self
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

	color.Green("✓ upgraded ghpm %s → %s", version, rel.TagName)
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0755)
}
