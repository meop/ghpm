package cli

import (
	"bytes"
	"os"
	"testing"

	"github.com/meop/golie/ui"
)

// TestGate_YesStartsTheWorkAfterABlank: --yes skips the confirm line, not the
// step: the work after a gate starts after a blank, as it does under a
// confirm, and the lines of the work run tight.
func TestGate_YesStartsTheWorkAfterABlank(t *testing.T) {
	var buf bytes.Buffer
	ui.SetOutput(&buf)
	ui.Reset()
	t.Cleanup(func() { ui.SetOutput(os.Stdout); ui.Reset() })
	yes = true
	t.Cleanup(func() { yes = false })

	if !gate([]string{"name"}, [][]string{{"fzf"}}, nil, "proceed?") {
		t.Fatal("gate declined under --yes")
	}
	print("fzf: asset fetching [fzf.tar.gz]...")
	print("fzf: bin found [fzf]")

	want := "name\n----\nfzf\n\nfzf: asset fetching [fzf.tar.gz]...\nfzf: bin found [fzf]\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
