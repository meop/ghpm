package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/store"
	"github.com/meop/ghpm/internal/toolchain"
)

// syncSheesh makes sure ghpm's vendored kebab is at exactly
// toolchain.SheeshVersion, fetching that release when it isn't. It is the
// counterpart to ghbin.Ensure, and follows the same rule: the check is the
// version, not the presence, so a copy left by an older or newer ghpm is
// replaced either way. Unlike gh's bootstrap it needs no raw-HTTP fetch of
// its own — gh's exists only because gh can't fetch itself, and anything
// reaching kebab already required gh first.
func syncSheesh(ctx context.Context, cfg *config.Settings, ghClient gh.Client) error {
	shimDir, err := store.ShimDir()
	if err != nil {
		return err
	}
	kebabPath := filepath.Join(shimDir, exeName("kebab"))
	if toolchain.Installed(kebabPath) == toolchain.SheeshVersion {
		return nil
	}
	// A dry run has nothing to preview here: the vendored toolchain isn't the
	// change the user asked about, and fetching it anyway would write to disk.
	if dryRun {
		return nil
	}

	rel, err := ghClient.GetReleaseByTag(ctx, config.RepoSheesh.Owner, config.RepoSheesh.Repo, toolchain.SheeshVersion)
	if err != nil {
		return err
	}
	_, tmpDir, cleanup, err := fetchSelected(ctx, cfg, ghClient, config.RepoSheesh, rel, binSheesh)
	if err != nil {
		return err
	}
	if cleanup == nil {
		return fmt.Errorf("no sheesh release asset found for this platform")
	}
	defer cleanup()

	return copyExecutablesToDir(tmpDir, shimDir)
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
