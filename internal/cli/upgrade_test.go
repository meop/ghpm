package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/store"
	"github.com/meop/golie/ui"
)

// runAs has upgrade take path for the running binary.
func runAs(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("ghpm"), 0755); err != nil {
		t.Fatal(err)
	}
	orig := self.Path
	self.Path = func() (string, error) { return path, nil }
	t.Cleanup(func() { self.Path = orig })
}

// runInstalled is runAs for the ghpm the install script leaves in the test
// home's bin dir, the one copy upgrade replaces.
func runInstalled(t *testing.T) {
	t.Helper()
	binDir, err := store.BinDir()
	if err != nil {
		t.Fatal(err)
	}
	runAs(t, filepath.Join(binDir, exeName(binGhpm)))
}

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
	runInstalled(t)
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
	runInstalled(t)
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
	runInstalled(t)
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

// TestRunUpgrade_RefusesAGhpmItsInstallerDidNotPlace: a ghpm that runs from
// anywhere but ~/.ghpm/bin was put there by something else — a package
// manager, go install, a build — and replacing it in place would leave that
// describing a binary it did not install. It is refused in one line, before
// the release page is asked anything, and whether or not the install script's
// own copy is also on the machine.
func TestRunUpgrade_RefusesAGhpmItsInstallerDidNotPlace(t *testing.T) {
	for name, alsoInstalled := range map[string]bool{"alone": false, "beside an installed copy": true} {
		t.Run(name, func(t *testing.T) {
			withHome(t)
			if alsoInstalled {
				runInstalled(t)
			}
			elsewhere := filepath.Join(t.TempDir(), exeName(binGhpm))
			runAs(t, elsewhere)
			writeSettings(t, &config.Settings{})

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("a ghpm that will not be replaced asked for %s", r.URL.Path)
			}))
			t.Cleanup(srv.Close)
			orig := self.ReleasesURL
			self.ReleasesURL = srv.URL + "/"
			t.Cleanup(func() { self.ReleasesURL = orig })

			var buf bytes.Buffer
			ui.SetOutput(&buf)
			t.Cleanup(func() { ui.SetOutput(os.Stdout) })

			if err := runUpgrade(cmdWithContext(), nil); err != errSilent {
				t.Fatalf("runUpgrade = %v, want the refusal", err)
			}
			if got, want := strings.TrimSpace(buf.String()), "✗ "+binGhpm+": cannot upgrade itself, running from "+elsewhere; got != want {
				t.Errorf("expected the single refusal line %q, got:\n%s", want, got)
			}
			if got, _ := os.ReadFile(elsewhere); string(got) != "ghpm" {
				t.Errorf("a ghpm the installer did not place was replaced: %q", got)
			}
		})
	}
}
