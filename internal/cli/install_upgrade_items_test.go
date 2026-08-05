package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/ui"
)

// TestInstallUpgradeItems_AggregateNotPerItem is the regression test for the
// bug reported against upgrade: components installed one at a time with a ✓
// line per component, instead of the parallel pool + single ✓ N summary that
// add/sync use. Fake install closures avoid ever touching the real gh/ghpm/
// sheesh install paths (one of which replaces the running binary).
func TestInstallUpgradeItems_AggregateNotPerItem(t *testing.T) {
	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	var concurrent, maxSeen atomic.Int64
	track := func() error {
		cur := concurrent.Add(1)
		defer concurrent.Add(-1)
		for {
			old := maxSeen.Load()
			if cur <= old || maxSeen.CompareAndSwap(old, cur) {
				break
			}
		}
		// Hold the goroutine open briefly so the other items' installs have a
		// window to overlap — without this, a fully-sequential bug and a truly
		// parallel pool are indistinguishable when each closure returns instantly.
		time.Sleep(20 * time.Millisecond)
		return nil
	}

	items := []upgradeItem{
		{name: "a", install: track},
		{name: "b", install: track},
		{name: "c", install: track},
	}

	if hadErrors := installUpgradeItems(context.Background(), &config.Settings{NumParallel: 3}, items); hadErrors {
		t.Fatal("expected no errors")
	}

	if maxSeen.Load() < 2 {
		t.Errorf("expected installs to run concurrently (max observed concurrency %d), not one at a time", maxSeen.Load())
	}

	out := buf.String()
	if strings.Count(out, "upgraded") != 1 {
		t.Errorf("expected exactly one aggregate summary line, got output:\n%s", out)
	}
	if !strings.Contains(out, "upgraded 3 component(s)") {
		t.Errorf("expected 'upgraded 3 component(s)', got:\n%s", out)
	}
	for _, name := range []string{"a", "b", "c"} {
		if strings.Contains(out, name+": upgraded") {
			t.Errorf("expected no per-component ✓ line for %q, got:\n%s", name, out)
		}
	}
}

func TestInstallUpgradeItems_PartialFailure(t *testing.T) {
	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	items := []upgradeItem{
		{name: "good", install: func() error { return nil }},
		{name: "bad", install: func() error { return fmt.Errorf("boom") }},
	}

	if hadErrors := installUpgradeItems(context.Background(), &config.Settings{NumParallel: 5}, items); !hadErrors {
		t.Fatal("expected hadErrors to be true")
	}

	out := buf.String()
	if !strings.Contains(out, "bad: boom") {
		t.Errorf("expected the failing component's error surfaced:\n%s", out)
	}
	if !strings.Contains(out, "upgraded 1 component(s)") {
		t.Errorf("expected the one successful component still counted in the aggregate line:\n%s", out)
	}
	if !strings.Contains(out, "✗ 1 component(s) failed") {
		t.Errorf("expected a closing failed-items table, not just the inline error, got:\n%s", out)
	}
	if !strings.Contains(out, "name") || !strings.Contains(out, "reason") {
		t.Errorf("expected the closing table to have name/reason columns, got:\n%s", out)
	}
}

// TestInstallUpgradeItems_TrailingSep locks in the same blank-line rule as
// downloadAllAssets: a block of per-item install output must not run tight
// into whatever the caller prints next, but must also not leave a trailing
// blank when it is genuinely the last output.
func TestInstallUpgradeItems_TrailingSep(t *testing.T) {
	t.Run("more output follows", func(t *testing.T) {
		var buf bytes.Buffer
		ui.SetOutput(&buf)
		t.Cleanup(func() { ui.SetOutput(os.Stdout) })

		items := []upgradeItem{{name: "a", install: func() error { return nil }}}
		if hadErrors := installUpgradeItems(context.Background(), &config.Settings{NumParallel: 5}, items); hadErrors {
			t.Fatal("expected no errors")
		}
		print("next section")

		want := "✓ upgraded 1 component(s)\n\nnext section\n"
		if got := buf.String(); got != want {
			t.Errorf("got %q, want %q (blank line before the next section)", got, want)
		}
	})

	t.Run("nothing follows, no trailing blank", func(t *testing.T) {
		var buf bytes.Buffer
		ui.SetOutput(&buf)
		t.Cleanup(func() { ui.SetOutput(os.Stdout) })

		items := []upgradeItem{{name: "a", install: func() error { return nil }}}
		if hadErrors := installUpgradeItems(context.Background(), &config.Settings{NumParallel: 5}, items); hadErrors {
			t.Fatal("expected no errors")
		}

		want := "✓ upgraded 1 component(s)\n"
		if got := buf.String(); got != want {
			t.Errorf("got %q, want %q (no trailing blank)", got, want)
		}
	})
}

func TestInstallUpgradeItems_AllFail_NoSummaryLine(t *testing.T) {
	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	var mu sync.Mutex
	items := []upgradeItem{
		{name: "a", install: func() error { mu.Lock(); defer mu.Unlock(); return fmt.Errorf("fail-a") }},
		{name: "b", install: func() error { mu.Lock(); defer mu.Unlock(); return fmt.Errorf("fail-b") }},
	}

	if hadErrors := installUpgradeItems(context.Background(), &config.Settings{NumParallel: 5}, items); !hadErrors {
		t.Fatal("expected hadErrors to be true")
	}

	out := buf.String()
	if strings.Contains(out, "upgraded") {
		t.Errorf("expected no aggregate summary line when nothing succeeded:\n%s", out)
	}
}
