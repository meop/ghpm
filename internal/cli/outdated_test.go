package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/ui"
)

// TestRunOutdated_ShowsUriTypeAndTarget guards outdated's table columns
// (see list_test.go's TestRunList_ShowsUriTypeAndTarget for why asset/
// artifact were dropped) — name/version/update/pin/uri/type/target.
func TestRunOutdated_ShowsUriTypeAndTarget(t *testing.T) {
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
	if !strings.Contains(out, "uri") || !strings.Contains(out, "type") || !strings.Contains(out, "target") {
		t.Errorf("expected \"uri\"/\"type\"/\"target\" column headers:\n%s", out)
	}
	if !strings.Contains(out, "bin") || !strings.Contains(out, "junegunn") {
		t.Errorf("expected the bin's type and uri in the table:\n%s", out)
	}
	if strings.Contains(out, "fzf-0.58.0-linux_amd64.tar.gz") || strings.Contains(out, "bin/fzf") {
		t.Errorf("asset/artifact should not appear in the table:\n%s", out)
	}
}
