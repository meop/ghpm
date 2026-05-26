package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/meop/ghpm/internal/config"
)

func writeSettings(t *testing.T, home string, s *config.Settings) {
	t.Helper()
	dir := filepath.Join(home, ".ghpm")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
}

func fakeGHBin(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "gh")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func TestInitCommand_Minimal(t *testing.T) {
	home := withHome(t)
	writeSettings(t, home, &config.Settings{})

	ci, err := initCommand(cmdOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ci.cfg == nil {
		t.Error("cfg is nil")
	}
	if ci.manifest != nil {
		t.Error("manifest should be nil without Manifest option")
	}
	if ci.unlock != nil {
		t.Error("unlock should be nil without Lock option")
	}
}

func TestInitCommand_WithManifest(t *testing.T) {
	home := withHome(t)
	writeSettings(t, home, &config.Settings{})
	writeManifest(t, home, &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0"}},
	})

	ci, err := initCommand(cmdOptions{Manifest: true})
	if err != nil {
		t.Fatal(err)
	}
	if ci.manifest == nil {
		t.Fatal("manifest is nil")
	}
	if _, ok := ci.manifest.Extracts["fzf"]; !ok {
		t.Error("fzf not in manifest")
	}
}

func TestInitCommand_WithLock(t *testing.T) {
	home := withHome(t)
	writeSettings(t, home, &config.Settings{})

	ci, err := initCommand(cmdOptions{Lock: true})
	if err != nil {
		t.Fatal(err)
	}
	if ci.unlock == nil {
		t.Fatal("unlock should be set with Lock option")
	}
	ci.close()
}

func TestInitCommand_GHCheckFails(t *testing.T) {
	home := withHome(t)
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	t.Setenv("HOME", home)
	writeSettings(t, home, &config.Settings{})

	_, err := initCommand(cmdOptions{GH: true})
	if err == nil {
		t.Fatal("expected error when gh not found")
	}
}

func TestInitCommand_NoVerifyPropagation(t *testing.T) {
	home := withHome(t)
	writeSettings(t, home, &config.Settings{NoVerify: true})
	noVerify = false
	defer func() { noVerify = false }()

	_, err := initCommand(cmdOptions{NoVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	if !noVerify {
		t.Error("noVerify should be true when settings say NoVerify")
	}
}

func TestInitCommand_ReposLoadFailure(t *testing.T) {
	home := withHome(t)
	writeSettings(t, home, &config.Settings{})

	ci, err := initCommand(cmdOptions{Repos: true})
	if err != nil {
		t.Fatal(err)
	}
	if ci.repos == nil {
		t.Error("repos should be empty map, not nil")
	}
}

func TestInitCommand_WithDirs(t *testing.T) {
	home := withHome(t)
	writeSettings(t, home, &config.Settings{})

	ci, err := initCommand(cmdOptions{Dirs: true})
	if err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(home, ".ghpm", "bin")
	if _, err := os.Stat(binDir); os.IsNotExist(err) {
		t.Error("bin dir was not created")
	}
	_ = ci
}
