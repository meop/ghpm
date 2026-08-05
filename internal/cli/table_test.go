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

// TestQuiet_SuppressesChatterNotContent locks in the -q design: print/
// printWarn/printPass are status chatter and get suppressed, but printFail
// (errors) and printTable (the actual content of gate previews and read-only
// commands like list/find/outdated) never do — quiet-ing those would make a
// read-only command return nothing at all.
func TestQuiet_SuppressesChatterNotContent(t *testing.T) {
	quiet = true
	defer func() { quiet = false }()

	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	print("info line")
	printWarn(nil, "warn line")
	printPass(nil, "pass line")
	printFail(nil, "fail line")
	printTable([]string{"name"}, [][]string{{"fzf"}}, nil)

	out := buf.String()
	for _, suppressed := range []string{"info line", "warn line", "pass line"} {
		if strings.Contains(out, suppressed) {
			t.Errorf("expected %q to be suppressed under quiet, got:\n%s", suppressed, out)
		}
	}
	for _, kept := range []string{"fail line", "fzf"} {
		if !strings.Contains(out, kept) {
			t.Errorf("expected %q to survive quiet, got:\n%s", kept, out)
		}
	}
}
