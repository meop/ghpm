package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/store"
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
