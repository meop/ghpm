package cli

import (
	"context"
	"fmt"
	"runtime"

	"github.com/meop/golie/pinned"
	"github.com/meop/golie/wedge"

	"github.com/meop/ghpm/internal/store"
)

// syncWedge makes sure ghpm's vendored mallet and its shim templates are
// exactly the wedge release golie pins, checked by content. It is the
// counterpart to ghbin.Ensure and follows the same rules (see golie's pinned
// package): a copy that does not match — another version's, a damaged one, a
// swapped one — is replaced from a plain download of the pinned release, and
// the directory ends up holding that release's files and nothing else.
func syncWedge(ctx context.Context) error {
	shimDir, err := store.ShimDir()
	if err != nil {
		return err
	}
	tool, ok := wedge.Tool(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return fmt.Errorf("no wedge release asset found for this platform")
	}
	// A dry run has nothing to preview here: the vendored toolchain isn't the
	// change the user asked about, and fetching it anyway would write to disk.
	if dryRun && !tool.Verified(shimDir) {
		return nil
	}
	return pinned.Ensure(ctx, tool, shimDir, pinned.Options{})
}
