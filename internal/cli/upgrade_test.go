package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/ui"
)

// ghpmOnlyGHBin fakes the vendored gh so every repo ghpm might ask about has
// an answer: ghpm at ghpmTag, plus a gh and a sheesh release at versions no
// upgrade output is allowed to mention.
func ghpmOnlyGHBin(t *testing.T, ghpmTag string) {
	t.Helper()
	release := func(tag, asset string) string {
		b, err := json.Marshal(map[string]any{
			"tagName": tag,
			"assets":  []map[string]any{{"name": asset, "size": 1234, "url": "https://x.com/a"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	fakeGHBin(t, `case "$*" in
  *meop/ghpm*) echo '`+release(ghpmTag, "ghpm-linux-amd64.tar.gz")+`' ;;
  *cli/cli*) echo '`+release("v2.67.0", "gh_2.67.0_linux_amd64.tar.gz")+`' ;;
  *meop/sheesh*) echo '`+release("v0.1.0", "sheesh-0.1.0-linux-x86_64.tar.gz")+`' ;;
  *) echo '{}' ;;
esac`)
}

func TestRunUpgrade_NoGH(t *testing.T) {
	withHome(t)
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	writeSettings(t, &config.Settings{})
	quiet = true
	defer func() { quiet = false }()

	err := runUpgrade(cmdWithExpiredContext(), nil)
	if err == nil {
		t.Fatal("expected error when gh not found")
	}
}

func TestRunUpgrade_AlreadyLatest(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	ghpmOnlyGHBin(t, "v"+version)

	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	if err := runUpgrade(cmdWithContext(), nil); err != nil {
		t.Fatal(err)
	}

	if got := buf.String(); got != msgGhpmUpToDate+"\n" {
		t.Errorf("expected the single up-to-date line, got %q", got)
	}
}

// TestRunUpgrade_OnlyGhpmIsAComponent is the regression test for the reported
// bug: upgrade offered to move gh and sheesh, ghpm's own vendored toolchain,
// which means nothing to someone who typed `ghpm upgrade` to upgrade ghpm.
// Both are pinned in internal/toolchain and synced at runtime instead, so
// neither may appear here — not even as an up-to-date row.
func TestRunUpgrade_OnlyGhpmIsAComponent(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	ghpmOnlyGHBin(t, "v9.9.9")

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
	if strings.Contains(out, binSheesh) || strings.Contains(out, "0.1.0") {
		t.Errorf("sheesh must never surface in upgrade, got:\n%s", out)
	}
	if strings.Contains(out, "2.67.0") {
		t.Errorf("gh must never surface in upgrade, got:\n%s", out)
	}
}

func TestUpgradeSelf_VersionComparison(t *testing.T) {
	if version == "dev" {
		t.Skip("version is dev, comparison logic differs")
	}
}
