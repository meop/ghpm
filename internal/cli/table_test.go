package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/ui"
)

// TestGate_DryRun_PrintsMessage locks in the fix for a dry run that rendered
// the preview table and then went silent — indistinguishable from a hang.
// gate() is the single shared choke point for add/sync/download/upgrade/remove,
// so this covers all five at once.
func TestGate_DryRun_PrintsMessage(t *testing.T) {
	dryRun = true
	defer func() { dryRun = false }()

	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	if gate([]string{"name"}, [][]string{{"fzf"}}, nil, "install 1 package(s)") {
		t.Fatal("dry run must never opt in")
	}

	out := buf.String()
	if !strings.Contains(out, "fzf") {
		t.Errorf("expected the preview table in dry-run output, got:\n%s", out)
	}
	if !strings.Contains(out, msgDryRun) {
		t.Errorf("expected the dry-run closing message, got:\n%s", out)
	}
	if strings.Contains(out, "install 1 package(s)") {
		t.Errorf("dry run must not reach the confirm prompt, got:\n%s", out)
	}
}

// TestGate_Confirmed_NoTrailingBlank confirms the real (non-dry-run) path is
// unaffected: table, then the confirm question, tight — no dry-run message.
func TestGate_Confirmed_NoTrailingBlank(t *testing.T) {
	var buf bytes.Buffer
	ui.SetOutput(&buf)
	ui.SetInput(strings.NewReader("y\n"))
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	if !gate([]string{"name"}, [][]string{{"fzf"}}, nil, "install 1 package(s)") {
		t.Fatal("expected confirm to opt in")
	}

	out := buf.String()
	if strings.Contains(out, msgDryRun) {
		t.Errorf("real run must not print the dry-run message, got:\n%s", out)
	}
}
