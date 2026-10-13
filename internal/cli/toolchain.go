package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/meop/ghpm/internal/fetch"
	"github.com/meop/ghpm/internal/store"
	"github.com/meop/ghpm/internal/toolchain"
)

// sheeshReleaseBase is where sheesh's releases are downloaded from: a plain
// release download addressed by the pinned version, not the GitHub API and
// not gh. It is a var, not a const, so a test can point it at a local server.
var sheeshReleaseBase = "https://github.com/meop/sheesh/releases/download/"

// syncSheesh makes sure ghpm's vendored kebab is at exactly
// toolchain.SheeshVersion, fetching that release when it isn't. It is the
// counterpart to ghbin.Ensure, and follows the same rules: the check is the
// version, not the presence, so a copy left by an older or newer ghpm is
// replaced either way; and the fetch is a plain download of a known URL,
// checked against the digest pinned with the version. The vendored directory
// is replaced whole — built beside it and swapped in — so nothing an older
// release shipped and this one does not is left lying in it.
func syncSheesh(ctx context.Context) error {
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

	name, sha256, ok := toolchain.SheeshAsset(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return fmt.Errorf("no sheesh release asset found for this platform")
	}

	vendorDir := filepath.Dir(shimDir)
	work, err := os.MkdirTemp(vendorDir, ".sheesh-sync-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()

	archive := filepath.Join(work, name)
	print("%s: asset fetching [%s]...", binSheesh, name)
	if err := fetch.Download(ctx, sheeshReleaseBase+"v"+toolchain.SheeshVersion+"/"+name, archive, sha256); err != nil {
		return fmt.Errorf("fetching sheesh %s: %w", toolchain.SheeshVersion, err)
	}
	unpacked := filepath.Join(work, "unpacked")
	if err := fetch.Extract(archive, unpacked, 0); err != nil {
		return err
	}
	fresh := filepath.Join(work, "fresh")
	if err := os.MkdirAll(fresh, 0755); err != nil {
		return err
	}
	if err := moveExecutablesToDir(unpacked, fresh); err != nil {
		return err
	}
	if err := os.RemoveAll(shimDir); err != nil {
		return err
	}
	return os.Rename(fresh, shimDir)
}

// moveExecutablesToDir walks srcDir recursively and moves all executable files
// (Unix: executable bit set; Windows: .exe suffix) to destDir flat.
func moveExecutablesToDir(srcDir, destDir string) error {
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
		if err := os.Rename(path, dest); err != nil {
			return err
		}
		return os.Chmod(dest, 0755)
	})
}
