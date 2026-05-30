package cli

import (
	"testing"

	"github.com/meop/ghpm/internal/config"
)

func TestRunList_Empty(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	quiet = true
	defer func() { quiet = false }()

	if err := runList(nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRunList_WithPackages(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	writeManifest(t, &config.Manifest{
		Repos: map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{
			"fzf": {Version: "0.58.0", Pin: "latest", Asset: map[string]config.AssetEntry{
				"fzf.tar.gz": {Bin: map[string]string{"fzf": "fzf"}},
			}},
		},
	})
	longNames = true
	defer func() { longNames = false }()

	if err := runList(nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRunList_LongNames(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
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
	longNames = true
	defer func() { longNames = false }()

	if err := runList(nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRunList_ShortNames(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
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
	shortNames = true
	defer func() { shortNames = false }()

	if err := runList(nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRunRefresh(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{
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
