package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/golie/ui"
)

// serveGhpmReleases stands up ghpm's release page with tag as its newest
// release, and points upgrade at it.
func serveGhpmReleases(t *testing.T, tag string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest" {
			http.Redirect(w, r, "/tag/"+tag, http.StatusFound)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	orig := self.ReleasesURL
	self.ReleasesURL = srv.URL + "/"
	t.Cleanup(func() { self.ReleasesURL = orig })
}

// TestRunUpgrade_NeedsNoGH: upgrade is a plain download from ghpm's own
// release page, so it works with no gh vendored and none on PATH — which is
// exactly when a broken ghpm most needs to be able to replace itself.
func TestRunUpgrade_NeedsNoGH(t *testing.T) {
	withHome(t)
	t.Setenv("PATH", t.TempDir())
	writeSettings(t, &config.Settings{})
	serveGhpmReleases(t, "v"+version)

	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	if err := runUpgrade(cmdWithContext(), nil); err != nil {
		t.Fatalf("upgrade needed something it should not have: %v", err)
	}
	if got := buf.String(); got != msgGhpmUpToDate+"\n" {
		t.Errorf("expected the single up-to-date line, got %q", got)
	}
}

func TestRunUpgrade_FailsWhenTheReleasePageCannotBeRead(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	quiet = true
	defer func() { quiet = false }()

	if err := runUpgrade(cmdWithExpiredContext(), nil); err == nil {
		t.Fatal("expected an error when the release page cannot be read")
	}
}

// TestRunUpgrade_OnlyGhpmIsAComponent is the regression test for the reported
// bug: upgrade offered to move gh and sheesh, ghpm's own vendored toolchain,
// which means nothing to someone who typed `ghpm upgrade` to upgrade ghpm.
// Both are pinned and synced at runtime instead, so neither may appear here —
// not even as an up-to-date row.
func TestRunUpgrade_OnlyGhpmIsAComponent(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	serveGhpmReleases(t, "v9.9.9")

	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	dryRun = true
	defer func() { dryRun = false }()

	if err := runUpgrade(cmdWithContext(), nil); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, binGhpm) || !strings.Contains(out, "9.9.9") {
		t.Errorf("expected ghpm's own update in the gate table, got:\n%s", out)
	}
	if strings.Contains(out, binSheesh) || strings.Contains(out, "gh ") {
		t.Errorf("only ghpm may surface in upgrade, got:\n%s", out)
	}
}
