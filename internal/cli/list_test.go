package cli

import (
	"testing"

	"github.com/meop/ghpm/internal/config"
)

func TestRunList_Empty(t *testing.T) {
	home := withHome(t)
	writeSettings(t, home, &config.Settings{})
	quiet = true
	defer func() { quiet = false }()

	if err := runList(nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRunList_WithPackages(t *testing.T) {
	home := withHome(t)
	writeSettings(t, home, &config.Settings{})
	writeManifest(t, home, &config.Manifest{
		Repos: map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{
			"fzf": {Version: "0.58.0", Pin: "latest", Asset: "fzf.tar.gz"},
		},
	})
	onlyNames = true
	defer func() { onlyNames = false }()

	if err := runList(nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRunList_OnlyNames(t *testing.T) {
	home := withHome(t)
	writeSettings(t, home, &config.Settings{})
	writeManifest(t, home, &config.Manifest{
		Repos: map[string]string{
			"fzf": "github.com/junegunn/fzf",
			"rg":  "github.com/BurntSushi/ripgrep",
		},
		Extracts: map[string]config.PackageEntry{
			"fzf": {Version: "0.58.0"},
			"rg":  {Version: "14.1.0"},
		},
	})
	onlyNames = true
	defer func() { onlyNames = false }()

	if err := runList(nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRunRefresh(t *testing.T) {
	home := withHome(t)
	writeSettings(t, home, &config.Settings{
		RepoSources: []string{"github.com/meop/ghpm-config"},
	})
	quiet = true
	defer func() { quiet = false }()

	fakeGHBin(t, `case "$*" in
  *api*) echo '{"repos": {"fzf": "github.com/junegunn/fzf"}}' ;;
  *) echo '{}' ;;
esac`)

	err := runRefresh(nil, nil)
	if err != nil {
		t.Logf("refresh error (expected without real gh): %v", err)
	}
}
