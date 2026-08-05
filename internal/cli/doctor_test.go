package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/ui"
)

// TestRunDoctor_QuietHidesPassNotFailWarn is the regression test for doctor
// bypassing -q entirely: every diagnostic line (PASS, FAIL, and WARN alike)
// used to route through the quiet-aware print(), so `doctor -q` suppressed
// real problems along with the banner header instead of showing only them.
// PASS is chatter ("nothing's wrong here") and should be suppressed; FAIL and
// WARN are exactly what a diagnostic command exists to surface under -q.
func TestRunDoctor_QuietHidesPassNotFailWarn(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	empty := t.TempDir()
	t.Setenv("PATH", empty)

	quiet = true
	defer func() { quiet = false }()

	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	if err := runDoctor(nil, nil); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if strings.Contains(out, "ghpm doctor") {
		t.Errorf("expected the banner header to be suppressed under quiet, got:\n%s", out)
	}
	if strings.Contains(out, "[PASS]") {
		t.Errorf("expected PASS lines to be suppressed under quiet, got:\n%s", out)
	}
	if !strings.Contains(out, "[FAIL]") {
		t.Errorf("expected FAIL lines to survive quiet (gh is not on PATH in this test), got:\n%s", out)
	}
}
