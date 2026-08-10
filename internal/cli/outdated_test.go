package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/ui"
)

// TestRunOutdated_ShowsRepoTypeAndTarget guards outdated's table columns
// (see list_test.go's TestRunList_ShowsRepoTypeAndTarget for why asset/
// artifact were dropped) — name/version/update/pin/repo/type/target.
func TestRunOutdated_ShowsRepoTypeAndTarget(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	writeManifest(t, &config.Manifest{
		Repos: map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{
			"fzf": {Version: "0.58.0", Pin: "latest", Assets: []string{"fzf-0.58.0-linux_amd64.tar.gz"}, Bin: map[string]string{"fzf": "bin/fzf"}},
		},
	})
	fakeGHBin(t, `echo '{"data":{"r0":{"latestRelease":{"tagName":"v0.60.0"}}}}'`)

	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	if err := runOutdated(cmdWithContext(), nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "repo") || !strings.Contains(out, "type") || !strings.Contains(out, "target") {
		t.Errorf("expected \"repo\"/\"type\"/\"target\" column headers:\n%s", out)
	}
	if !strings.Contains(out, "bin") || !strings.Contains(out, "junegunn") {
		t.Errorf("expected the bin's type and repo in the table:\n%s", out)
	}
	if strings.Contains(out, "fzf-0.58.0-linux_amd64.tar.gz") || strings.Contains(out, "bin/fzf") {
		t.Errorf("asset/artifact should not appear in the table:\n%s", out)
	}
}
