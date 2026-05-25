package cli

import (
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

	hadErrors := false

	if err := upgradeGh(cfg); err != nil {
		printFail(cfg, "gh: %v", err)
		hadErrors = true
	}
	if err := upgradeSelf(cfg); err != nil {
		printFail(cfg, "ghpm: %v", err)
		hadErrors = true
	}
	if err := upgradeShim(cfg); err != nil {
		printFail(cfg, "sheesh: %v", err)
		hadErrors = true
	}

	if hadErrors {
		return errSilent
	}
	return nil
}

func upgradeSelf(cfg *config.Settings) error {
	rel, err := gh.GetLatestRelease("meop", binGhpm)
	if err != nil {
		return err
	}
	latestVer := config.NormalizeVersion(rel.TagName)

	sep()
	if strings.TrimPrefix(rel.TagName, "v") == strings.TrimPrefix(version, "v") {
		fmt.Printf("ghpm: %s is already the latest\n", version)
		return nil
	}
	fmt.Printf("ghpm: upgrading %s → %s\n", version, latestVer)

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
		fmt.Printf("[dry-run] would upgrade ghpm %s → %s (asset: %s)\n", version, latestVer, chosen.Name)
		return nil
	}

	if !promptConfirm(fmt.Sprintf("upgrade ghpm %s → %s", version, latestVer)) {
		return nil
	}

	cacheDir, err := store.ReleaseDir("github.com/meop/ghpm", rel.TagName)
	if err != nil {
		return err
	}
	if err := gh.DownloadAsset("meop", binGhpm, rel.TagName, chosen.Name, cacheDir); err != nil {
		return err
	}
	if !noVerify {
		_, _ = asset.Verify("meop", binGhpm, rel.TagName, cacheDir, chosen.Name)
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

	printPass(cfg, "upgraded %s → %s", version, latestVer)
	return nil
}

func upgradeGh(cfg *config.Settings) error {
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

	rel, err := gh.GetLatestRelease("cli", "cli")
	if err != nil {
		return err
	}
	latestVer := config.NormalizeVersion(rel.TagName)

	sep()
	if currentVer == latestVer {
		fmt.Printf("gh: %s is already the latest\n", currentVer)
		return nil
	}
	fmt.Printf("gh: upgrading %s → %s\n", currentVer, latestVer)

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
		fmt.Printf("[dry-run] would upgrade gh %s → %s (asset: %s)\n", currentVer, latestVer, chosen.Name)
		return nil
	}

	if !promptConfirm(fmt.Sprintf("upgrade gh %s → %s", currentVer, latestVer)) {
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
		_, _ = asset.Verify("cli", "cli", rel.TagName, cacheDir, chosen.Name)
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
	if err := copyFile(ghBin, ghPath); err != nil {
		return err
	}
	if err := os.Chmod(ghPath, 0755); err != nil {
		return err
	}

	printPass(cfg, "upgraded %s → %s", currentVer, latestVer)
	return nil
}

func upgradeShim(cfg *config.Settings) error {
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

	rel, err := gh.GetLatestRelease("meop", binSheesh)
	if err != nil {
		return err
	}
	latestVer := config.NormalizeVersion(rel.TagName)

	sep()
	if currentVer == latestVer {
		fmt.Printf("sheesh: %s is already the latest\n", currentVer)
		return nil
	}
	if currentVer == "" {
		fmt.Printf("sheesh: installing %s\n", latestVer)
	} else {
		fmt.Printf("sheesh: upgrading %s → %s\n", currentVer, latestVer)
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

	var prompt string
	if currentVer == "" {
		prompt = fmt.Sprintf("install sheesh %s", latestVer)
	} else {
		prompt = fmt.Sprintf("upgrade sheesh %s → %s", currentVer, latestVer)
	}

	if dryRun {
		fmt.Printf("[dry-run] would %s (asset: %s)\n", prompt, chosen.Name)
		return nil
	}

	if !promptConfirm(prompt) {
		return nil
	}

	cacheDir, err := store.ReleaseDir("github.com/meop/sheesh", rel.TagName)
	if err != nil {
		return err
	}
	if err := gh.DownloadAsset("meop", binSheesh, rel.TagName, chosen.Name, cacheDir); err != nil {
		return err
	}
	if !noVerify {
		_, _ = asset.Verify("meop", binSheesh, rel.TagName, cacheDir, chosen.Name)
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
		printPass(cfg, "installed %s", latestVer)
	} else {
		printPass(cfg, "upgraded %s → %s", currentVer, latestVer)
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

func replaceSelf(src, dst string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(src, dst)
	}
	bak := filepath.Join(os.TempDir(), filepath.Base(dst)+".bak")
	_ = os.Remove(bak)
	if err := os.Rename(dst, bak); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		_ = os.Rename(bak, dst)
		return err
	}
	return nil
}
