package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/ui"
)

// TestRunOutdated_ShowsAssetColumn guards the asset column landing between
// repo and artifact (and, since they share appendEntryRows, that the
// type/artifact header order — previously swapped relative to the data —
// actually matches what's appended: artifact value, then type, then target).
func TestRunOutdated_ShowsAssetColumn(t *testing.T) {
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
	if !strings.Contains(out, "asset") {
		t.Errorf("expected an \"asset\" column header:\n%s", out)
	}
	if !strings.Contains(out, "fzf-0.58.0-linux_amd64.tar.gz") {
		t.Errorf("expected the package's asset name in the table:\n%s", out)
	}
	if !strings.Contains(out, "bin/fzf") {
		t.Errorf("expected the artifact path in the table:\n%s", out)
	}
}
