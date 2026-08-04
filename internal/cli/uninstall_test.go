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
	"github.com/meop/ghpm/internal/ui"
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

	m, err := config.LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Extracts["fzf"]; !ok {
		t.Error("dry-run should not actually remove the package")
	}
}
