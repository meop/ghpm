package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/meop/golie/ui"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/faketool"
	"github.com/meop/ghpm/internal/toolchain"
)

// TestRunRefresh_SaysEachThingOnALineOfItsOwn: refresh says what it did to
// each vendored tool and each repo source on one line, labelled first, with no
// blank line between them. A download says so on the line before, a repo
// source's included, and a repo source's version is its file's git hash.
func TestRunRefresh_SaysEachThingOnALineOfItsOwn(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{RepoSources: []string{"github.com/meop/ghpm-config"}})
	fakeGHBin(t, `case "$*" in
  *"contents --jq"*) echo 0000000000000000000000000000000000000000 ;;
  *"contents/repo.toml"*) printf '[fzf]\nuri = "github.com/junegunn/fzf"\n' ;;
  *) exit 0 ;;
esac`)
	ghURL, _ := faketool.Release(t, "0.0.0", nil, "")
	orig := toolchain.GhReleasesURL
	toolchain.GhReleasesURL = ghURL
	t.Cleanup(func() { toolchain.GhReleasesURL = orig })
	serveWedgeRelease(t, "2.0.0", fakeMalletBody)

	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })
	if err := runPull(nil, nil); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	want := []string{
		"✓ vendor - gh: up to date [0.0.0]",
		"vendor - wedge: asset fetching [",
		"✓ vendor - wedge: installed [2.0.0]",
		"repo - github.com/meop/ghpm-config: asset fetching [repo.toml]...",
		"✓ repo - github.com/meop/ghpm-config: installed [",
	}
	if len(lines) != len(want) {
		t.Fatalf("refresh printed %d lines, want %d:\n%s", len(lines), len(want), buf.String())
	}
	for i, w := range want {
		if !strings.HasPrefix(lines[i], w) {
			t.Errorf("line %d is %q, want it to start %q", i+1, lines[i], w)
		}
	}
}
