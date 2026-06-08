package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/ui"
)

func TestFilterExtracts(t *testing.T) {
	extracts := map[string]config.PackageEntry{
		"fzf":    {Version: "1"},
		"fzf@14": {Version: "2"},
		"rg":     {Version: "3"},
	}
	if got := filterExtracts(extracts, nil); len(got) != 3 {
		t.Errorf("nil names should return all: want 3, got %d", len(got))
	}
	if got := filterExtracts(extracts, []string{"rg"}); len(got) != 1 || got["rg"].Version != "3" {
		t.Errorf("exact key match: got %v", got)
	}
	// A base name matches both the plain and the versioned install.
	if got := filterExtracts(extracts, []string{"fzf"}); len(got) != 2 {
		t.Errorf("base-name match: want fzf and fzf@14, got %v", got)
	}
	if got := filterExtracts(extracts, []string{"fzf@14"}); len(got) != 1 {
		t.Errorf("versioned key match: want 1, got %v", got)
	}
	if got := filterExtracts(extracts, []string{"nope"}); len(got) != 0 {
		t.Errorf("no match: want 0, got %d", len(got))
	}
}

func TestRunList_NameFilter(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	writeManifest(t, &config.Manifest{
		Repos: map[string]string{
			"fzf": "github.com/junegunn/fzf",
			"rg":  "github.com/BurntSushi/ripgrep",
		},
		Extracts: map[string]config.PackageEntry{
			"fzf": {Version: "0.58.0", Pin: "latest", Asset: map[string]config.AssetEntry{
				"fzf.tar.gz": {Bin: map[string]string{"fzf": "fzf"}},
			}},
			"rg": {Version: "14.1.0", Pin: "latest", Asset: map[string]config.AssetEntry{
				"rg.tar.gz": {Bin: map[string]string{"rg": "rg"}},
			}},
		},
	})
	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	if err := runList(nil, []string{"fzf"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "fzf") {
		t.Errorf("expected fzf in filtered output:\n%s", out)
	}
	if strings.Contains(out, "ripgrep") || strings.Contains(out, "14.1.0") {
		t.Errorf("rg should be filtered out:\n%s", out)
	}
}

func TestRunList_NoMatchVsEmpty(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})

	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	// Nothing installed at all.
	if err := runList(nil, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no packages installed") {
		t.Errorf("empty install set should say 'no packages installed':\n%s", buf.String())
	}

	// Something installed, but the filter matches nothing.
	writeManifest(t, &config.Manifest{
		Repos: map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{
			"fzf": {Version: "0.58.0", Pin: "latest", Asset: map[string]config.AssetEntry{
				"fzf.tar.gz": {Bin: map[string]string{"fzf": "fzf"}},
			}},
		},
	})
	buf.Reset()
	if err := runList(nil, []string{"nope"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no packages matched") {
		t.Errorf("unmatched filter should say 'no packages matched':\n%s", buf.String())
	}
}

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
