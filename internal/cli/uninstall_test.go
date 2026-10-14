package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/store"
	"github.com/meop/golie/ui"
)

func writeManifest(t *testing.T, m *config.Manifest) {
	t.Helper()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := store.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRunUninstall_RemovesFromManifest(t *testing.T) {
	withHome(t)
	yes = true
	defer func() { yes = false }()

	writeManifest(t, &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0"}},
	})

	if err := runRemove(nil, []string{"fzf"}); err != nil {
		t.Fatal(err)
	}

	m, err := config.LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Extracts["fzf"]; ok {
		t.Error("fzf still in manifest extracts")
	}
	if _, ok := m.Repos["fzf"]; ok {
		t.Error("fzf still in manifest repos")
	}
}

func TestRunUninstall_KeepsRepoWhenOtherVersionExists(t *testing.T) {
	withHome(t)
	yes = true
	defer func() { yes = false }()

	writeManifest(t, &config.Manifest{
		Repos: map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{
			"fzf":        {Version: "0.58.0"},
			"fzf@0.57.0": {Version: "0.57.0"},
		},
	})

	if err := runRemove(nil, []string{"fzf"}); err != nil {
		t.Fatal(err)
	}

	m, err := config.LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Extracts["fzf"]; ok {
		t.Error("fzf still in extracts")
	}
	if _, ok := m.Repos["fzf"]; !ok {
		t.Error("fzf removed from repos but fzf@0.57.0 is still installed")
	}
}

func TestRunRemove_AggregateNotPerItem(t *testing.T) {
	withHome(t)
	yes = true
	defer func() { yes = false }()

	writeManifest(t, &config.Manifest{
		Repos: map[string]string{
			"fzf": "github.com/junegunn/fzf",
			"rg":  "github.com/BurntSushi/ripgrep",
		},
		Extracts: map[string]config.PackageEntry{
			"fzf": {Version: "0.58.0"},
			"rg":  {Version: "14.1.0"},
		},
	})

	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	if err := runRemove(nil, []string{"fzf", "rg"}); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "uninstalled 2 package(s)") {
		t.Errorf("expected one aggregate summary line, got:\n%s", out)
	}
	if strings.Contains(out, "fzf: uninstalled") || strings.Contains(out, "rg: uninstalled") {
		t.Errorf("expected no per-package ✓ line, got:\n%s", out)
	}
}

func TestRunRemove_DryRunShowsGateTable(t *testing.T) {
	withHome(t)
	writeManifest(t, &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0"}},
	})

	dryRun = true
	defer func() { dryRun = false }()

	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	if err := runRemove(nil, []string{"fzf"}); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "name") || !strings.Contains(out, "version") || !strings.Contains(out, "assets") {
		t.Errorf("expected the real gate table (with headers) in dry-run output, not a bespoke text listing:\n%s", out)
	}
	if strings.Contains(out, "uninstalled") {
		t.Errorf("dry-run should bail before doing or reporting any work:\n%s", out)
	}
	if !strings.Contains(out, msgDryRun) {
		t.Errorf("expected the dry-run closing message so a bare table isn't mistaken for a hang, got:\n%s", out)
	}

	m, err := config.LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Extracts["fzf"]; !ok {
		t.Error("dry-run should not actually remove the package")
	}
}

// TestRunUninstall_RefusesGhpmItself: ghpm is never one of its own packages,
// so naming it is a failure said of the verb asked for — not "not installed",
// which reads as though adding it were the next step — and the packages named
// beside it are still removed.
func TestRunUninstall_RefusesGhpmItself(t *testing.T) {
	withHome(t)
	yes = true
	defer func() { yes = false }()
	writeManifest(t, &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0"}},
	})

	const refusal = "✗ ghpm: cannot remove itself\n"
	for _, args := range [][]string{{binGhpm}, {binGhpm + "@1"}, {binGhpm, "fzf"}} {
		var buf bytes.Buffer
		ui.SetOutput(&buf)
		t.Cleanup(func() { ui.SetOutput(os.Stdout) })

		if err := runRemove(nil, args); err != errSilent {
			t.Fatalf("remove %v = %v, want the failure", args, err)
		}
		out := buf.String()
		if !strings.Contains(out, refusal) {
			t.Errorf("remove %v: no refusal in:\n%s", args, out)
		}
		// alone, the line is the whole run; the failed table is for a run
		// that had other packages to get through
		if alone := len(args) == 1; alone && out != refusal {
			t.Errorf("remove %v: expected the single refusal line, got:\n%s", args, out)
		} else if !alone && !strings.Contains(out, "1 package(s) failed") {
			t.Errorf("remove %v: no failed table in:\n%s", args, out)
		}
	}

	m, err := config.LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Extracts["fzf"]; ok {
		t.Error("fzf, named beside ghpm, was not removed")
	}
}
