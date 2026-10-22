package cli

import (
	"context"
	"fmt"
	"runtime"

	"github.com/meop/golie/pinned"
	"github.com/meop/golie/sheesh"

	"github.com/meop/ghpm/internal/store"
)

// syncSheesh makes sure ghpm's vendored kebab and its shim templates are
// exactly the sheesh release golie pins, checked by content. It is the
// counterpart to ghbin.Ensure and follows the same rules (see golie's pinned
// package): a copy that does not match — another version's, a damaged one, a
// swapped one — is replaced from a plain download of the pinned release, and
// the directory ends up holding that release's files and nothing else.
func syncSheesh(ctx context.Context) error {
	shimDir, err := store.ShimDir()
	if err != nil {
		return err
	}
	tool, ok := sheesh.Tool(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return fmt.Errorf("no sheesh release asset found for this platform")
	}
	if tool.Verified(shimDir) {
		return nil
	}
	// A dry run has nothing to preview here: the vendored toolchain isn't the
	// change the user asked about, and fetching it anyway would write to disk.
	if dryRun {
		return nil
	}
	// silent: it is ghpm's internals, not something its user asked for
	return pinned.Ensure(ctx, tool, shimDir, pinned.Options{})
}
