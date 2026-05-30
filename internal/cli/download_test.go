package cli

import (
	"testing"

	"github.com/meop/ghpm/internal/config"
)

func TestRunDownload_NoGH(t *testing.T) {
	withHome(t)
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	writeSettings(t, &config.Settings{})
	quiet = true
	defer func() { quiet = false }()

	err := runDownload(cmdWithContext(), []string{"fzf"})
	if err == nil {
		t.Fatal("expected error when gh not found")
	}
}

func TestRunInfo_NoGH(t *testing.T) {
	withHome(t)
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	writeSettings(t, &config.Settings{})
	quiet = true
	defer func() { quiet = false }()

	err := runInfo(cmdWithContext(), []string{"fzf"})
	if err == nil {
		t.Fatal("expected error when gh not found")
	}
}

func TestRunOutdated_NoGH(t *testing.T) {
	withHome(t)
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	writeSettings(t, &config.Settings{})
	writeManifest(t, &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0"}},
	})

	err := runOutdated(cmdWithContext(), []string{})
	if err == nil {
		t.Fatal("expected error when gh not found")
	}
}
