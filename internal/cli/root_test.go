package cli

import (
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/ui"
)

// TestNonInteractiveFlag_SetsUIOverride covers the wiring, not just the
// override itself (see internal/ui for that): parsing --non-interactive
// through a real cobra Execute must reach ui.Interactive() before the
// subcommand runs.
func TestNonInteractiveFlag_SetsUIOverride(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	t.Cleanup(func() {
		nonInteractive = false
		ui.SetNonInteractive(false)
	})

	root := NewRootCmd()
	root.SetArgs([]string{"list", "--non-interactive"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if ui.Interactive() {
		t.Error("expected --non-interactive to force ui.Interactive() false")
	}
}

func TestNonInteractiveFlag_DefaultLeavesInteractiveAlone(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	t.Cleanup(func() {
		nonInteractive = false
		ui.SetNonInteractive(false)
	})

	root := NewRootCmd()
	root.SetArgs([]string{"list"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if nonInteractive {
		t.Error("expected --non-interactive to default false")
	}
}
