package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/store"
)

type ctxKey struct{}

func cmdWithContext() *cobra.Command {
	cmd := &cobra.Command{}
	ctx := context.WithValue(context.Background(), ctxKey{}, true)
	cmd.SetContext(ctx)
	return cmd
}

// cmdWithExpiredContext is for tests asserting gh is NOT found: an already-
// expired context fails the vendor bootstrap's fetch without touching the
// network, deterministically simulating "offline" rather than actually
// racing the real GitHub API.
func cmdWithExpiredContext() *cobra.Command {
	cmd := &cobra.Command{}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now())
	cancel()
	cmd.SetContext(ctx)
	return cmd
}

// writeFakeShim stamps what shim.IsShim looks for, so a test fixture is a shim
// ghpm would recognize as its own rather than a stranger's binary.
func writeFakeShim(t *testing.T, path string) {
	t.Helper()
	extracts, err := store.ExtractsDir()
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("use kebab to stamp a source path\x00" + filepath.Join(extracts, "pkg", "1.0", "bin"))
	if err := os.WriteFile(path, body, 0755); err != nil {
		t.Fatal(err)
	}
}
