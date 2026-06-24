package asset

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/ui"
)

func writeFakeBinary(t *testing.T, dir, name string) {
	t.Helper()
	fname := name
	if runtime.GOOS == "windows" {
		fname += ".exe"
	}
	path := filepath.Join(dir, fname)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	var magic []byte
	switch runtime.GOOS {
	case "windows":
		magic = []byte("MZ")
	case "darwin":
		magic = []byte{0xce, 0xfa, 0xed, 0xfe}
	default:
		magic = []byte{0x7f, 'E', 'L', 'F', 0}
	}
	if err := os.WriteFile(path, magic, 0755); err != nil {
		t.Fatal(err)
	}
}

func fakeStdin(t *testing.T, input string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	ui.SetInput(r)
	t.Cleanup(func() {
		os.Stdin = oldStdin
		ui.SetInput(oldStdin)
	})
	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
}

func TestFindBins_Root(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, dir, "mytool")
	got := FindBins(dir)
	if len(got) != 1 || got[0].BinDir != "" || got[0].BinName != "mytool" {
		t.Errorf("got %v, want [{BinDir:%q BinName:%q}]", got, "", "mytool")
	}
}

func TestFindBins_BinSubdir(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, filepath.Join(dir, "bin"), "mytool")
	got := FindBins(dir)
	if len(got) != 1 || got[0].BinDir != "bin" || got[0].BinName != "mytool" {
		t.Errorf("got %v", got)
	}
}

func TestFindBins_Subdir(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, filepath.Join(dir, "mytool-1.0"), "mytool")
	got := FindBins(dir)
	if len(got) != 1 || got[0].BinDir != "mytool-1.0" || got[0].BinName != "mytool" {
		t.Errorf("got %v", got)
	}
}

func TestFindBins_SubdirBin(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, filepath.Join(dir, "mytool-1.0", "bin"), "mytool")
	got := FindBins(dir)
	if len(got) != 1 || got[0].BinDir != "mytool-1.0/bin" || got[0].BinName != "mytool" {
		t.Errorf("got %v", got)
	}
}

func TestFindBins_NotFound(t *testing.T) {
	dir := t.TempDir()
	got := FindBins(dir)
	if len(got) != 0 {
		t.Errorf("expected no results, got %v", got)
	}
}

func TestFindBins_Multiple(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, dir, "mytool")
	writeFakeBinary(t, dir, "mytool-extra")
	got := FindBins(dir)
	if len(got) != 2 {
		t.Errorf("expected 2 results, got %d: %v", len(got), got)
	}
}

func TestFindBins_ReturnsAllExecutables(t *testing.T) {
	dir := t.TempDir()
	// FindBins is pure discovery — no name filter — so binaries that don't echo
	// the package name (llama-cli, rpc-server for "llama.cpp") are all returned;
	// ranking happens later in SelectBins.
	writeFakeBinary(t, dir, "llama-cli")
	writeFakeBinary(t, dir, "llama-server")
	writeFakeBinary(t, dir, "rpc-server")
	got := FindBins(dir)
	if len(got) != 3 {
		t.Errorf("expected all 3 executables, got %d: %v", len(got), got)
	}
}

func TestFindBins_ExcludesSharedLibrary(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, dir, "foo")
	// A shared library carries ELF/Mach-O magic too, but must not be offered as
	// a bin. (On Windows the .exe-suffix rule already excludes it.)
	soName := "libfoo.so"
	if runtime.GOOS == "darwin" {
		soName = "libfoo.dylib"
	}
	if err := os.WriteFile(filepath.Join(dir, soName), []byte{0x7f, 'E', 'L', 'F', 0}, 0755); err != nil {
		t.Fatal(err)
	}
	got := FindBins(dir)
	if len(got) != 1 {
		t.Errorf("expected only the executable, got %d: %v", len(got), got)
	}
}

func TestSelectBins_None(t *testing.T) {
	got, err := SelectBins(nil, nil, "")
	if got != nil || err != nil {
		t.Errorf("expected nil,nil; got %v,%v", got, err)
	}
}

func TestSelectBins_One(t *testing.T) {
	c := []BinCandidate{{BinDir: "", BinName: "tool"}}
	got, err := SelectBins(c, nil, "")
	if err != nil || len(got) != 1 || got[0].BinName != "tool" {
		t.Errorf("got %v,%v", got, err)
	}
}

func TestSelectBins_SamePrevNames(t *testing.T) {
	c := []BinCandidate{{BinName: "uv"}, {BinName: "uvx"}}
	got, err := SelectBins(c, []string{"uv", "uvx"}, "")
	if err != nil || len(got) != 2 {
		t.Errorf("expected auto-select all; got %v,%v", got, err)
	}
}

func TestSelectBins_PromptAll(t *testing.T) {
	fakeStdin(t, "\n")
	c := []BinCandidate{{BinName: "uv"}, {BinName: "uvx"}}
	got, err := SelectBins(c, nil, "")
	if err != nil || len(got) != 2 {
		t.Errorf("expected 2; got %v,%v", got, err)
	}
}

func TestSelectBins_PromptSkip(t *testing.T) {
	fakeStdin(t, "0\n")
	c := []BinCandidate{{BinName: "uv"}, {BinName: "uvx"}}
	_, err := SelectBins(c, nil, "")
	if err != ErrSkip {
		t.Errorf("expected ErrSkip, got %v", err)
	}
}

func TestSelectBins_PromptSubset(t *testing.T) {
	fakeStdin(t, "1\n")
	c := []BinCandidate{{BinName: "uv"}, {BinName: "uvx"}}
	got, err := SelectBins(c, nil, "")
	if err != nil || len(got) != 1 || got[0].BinName != "uv" {
		t.Errorf("expected [uv]; got %v,%v", got, err)
	}
}

func TestNameStem(t *testing.T) {
	cases := map[string]string{
		"llama.cpp": "llama",
		"ast-grep":  "ast",
		"ripgrep":   "ripgrep",
		"node.js":   "node",
		"Foo-Bar":   "foo",
		"s5cmd":     "s5cmd", // digits stay in the stem
		"7zip":      "7zip",
		".hidden":   "", // leading separator → empty stem disables ranking
	}
	for in, want := range cases {
		if got := nameStem(in); got != want {
			t.Errorf("nameStem(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSelectBins_PreferredShortList(t *testing.T) {
	// "llama.cpp" → stem "llama": llama-* are preferred, rpc-server hidden.
	fakeStdin(t, "\n") // empty selects all preferred, not the hidden rpc-server
	c := []BinCandidate{{BinName: "llama-cli"}, {BinName: "llama-server"}, {BinName: "rpc-server"}}
	got, err := SelectBins(c, nil, "llama.cpp")
	if err != nil || len(got) != 2 {
		t.Fatalf("expected 2 preferred bins; got %v,%v", got, err)
	}
	for _, b := range got {
		if b.BinName == "rpc-server" {
			t.Errorf("rpc-server should be hidden, not selected by default: %v", got)
		}
	}
}

func TestSelectBins_ShowMoreRevealsHidden(t *testing.T) {
	// Pick "show more" (index 3 = after the two preferred), then rpc-server (3) from the full list.
	fakeStdin(t, "3\n3\n")
	c := []BinCandidate{{BinName: "llama-cli"}, {BinName: "llama-server"}, {BinName: "rpc-server"}}
	got, err := SelectBins(c, nil, "llama.cpp")
	if err != nil || len(got) != 1 || got[0].BinName != "rpc-server" {
		t.Errorf("expected [rpc-server] via show more; got %v,%v", got, err)
	}
}

// TestSelectBins_PromptLabelAndBlank guards the prompt UX: when a selection
// menu interrupts a stream of progress output (e.g. during `up`), it must be
// preceded and followed by a blank line and name the package it belongs to.
func TestSelectBins_PromptLabelAndBlank(t *testing.T) {
	fakeStdin(t, "3\n")
	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })
	// Prior progress output so the deferred Break is not a no-op.
	ui.Out("caddy: found bin [caddy.exe]")
	c := []BinCandidate{{BinName: "codex-command-runner"}, {BinName: "codex-windows-sandbox-setup"}, {BinName: "codex"}}
	if _, err := SelectBins(c, nil, "codex"); err != nil {
		t.Fatal(err)
	}
	// Resumed progress output flushes the prompt's trailing Break as a blank.
	ui.Out("uv: found bin [uv.exe]")
	if !strings.Contains(buf.String(), "caddy.exe]\n\ncodex: choose bin(s)\n") {
		t.Errorf("missing blank line and/or label before menu:\n%q", buf.String())
	}
	// In the buffer the user's echoed Enter is absent, so the trailing blank
	// shows as the prompt line followed by a single newline before "uv".
	if !strings.Contains(buf.String(), "): \nuv: found bin [uv.exe]") {
		t.Errorf("missing blank line after menu:\n%q", buf.String())
	}
}

// TestPromptBinNames_LabelInHeader guards that the rename-conflict prompt, which
// shares the tight sync/install loops with the selection menu, also names its
// package so it is identifiable when it interrupts a progress stream.
func TestPromptBinNames_LabelInHeader(t *testing.T) {
	fakeStdin(t, "bun-new\n")
	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })
	if _, err := PromptBinNames([]string{"bin/bun"}, []string{"bun"}, map[string]string{"bun": "other-pkg"}, "bun"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "bun: bin conflicts — rename required:") {
		t.Errorf("missing package label in conflict header:\n%q", buf.String())
	}
}

func TestParseMultiSelect_Empty(t *testing.T) {
	got, err := parseMultiSelect("", 3)
	if err != nil || len(got) != 3 {
		t.Errorf("empty input should return all 3; got %v,%v", got, err)
	}
}

func TestParseMultiSelect_Zero(t *testing.T) {
	got, err := parseMultiSelect("0", 3)
	if got != nil || err != nil {
		t.Errorf("0 should return nil,nil; got %v,%v", got, err)
	}
}

func TestParseMultiSelect_Single(t *testing.T) {
	got, err := parseMultiSelect("2", 3)
	if err != nil || len(got) != 1 || got[0] != 2 {
		t.Errorf("got %v,%v", got, err)
	}
}

func TestParseMultiSelect_Comma(t *testing.T) {
	got, err := parseMultiSelect("1,3", 3)
	if err != nil || len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Errorf("got %v,%v", got, err)
	}
}

func TestParseMultiSelect_Space(t *testing.T) {
	got, err := parseMultiSelect("1 3", 3)
	if err != nil || len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Errorf("got %v,%v", got, err)
	}
}

func TestParseMultiSelect_Range(t *testing.T) {
	got, err := parseMultiSelect("2-4", 5)
	if err != nil || len(got) != 3 || got[0] != 2 || got[1] != 3 || got[2] != 4 {
		t.Errorf("got %v,%v", got, err)
	}
}

func TestParseMultiSelect_Invalid(t *testing.T) {
	_, err := parseMultiSelect("abc", 3)
	if err == nil {
		t.Error("expected error for invalid input")
	}
}

func writeFakeFont(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fake font"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestFindFonts_Root(t *testing.T) {
	dir := t.TempDir()
	writeFakeFont(t, dir, "Hack-Regular.ttf")
	got := FindFonts(dir)
	if len(got) != 1 || got[0].FontDir != "" || got[0].FontName != "Hack-Regular.ttf" {
		t.Errorf("got %v", got)
	}
}

func TestFindFonts_Subdir(t *testing.T) {
	dir := t.TempDir()
	writeFakeFont(t, dir, filepath.Join("fonts", "Hack-Regular.ttf"))
	got := FindFonts(dir)
	if len(got) != 1 || got[0].FontDir != "fonts" || got[0].FontName != "Hack-Regular.ttf" {
		t.Errorf("got %v", got)
	}
}

func TestFindFonts_AllExtensions(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.ttf", "b.otf", "c.woff", "d.woff2"} {
		writeFakeFont(t, dir, name)
	}
	writeFakeFont(t, dir, "ignored.txt")
	writeFakeFont(t, dir, "ignored.exe")
	got := FindFonts(dir)
	if len(got) != 4 {
		t.Errorf("expected 4 font files, got %d: %v", len(got), got)
	}
}

func TestFindFonts_Empty(t *testing.T) {
	dir := t.TempDir()
	got := FindFonts(dir)
	if len(got) != 0 {
		t.Errorf("expected empty result, got %v", got)
	}
}

func TestSelectFonts_None(t *testing.T) {
	got, err := SelectFonts(nil, nil, "")
	if got != nil || err != nil {
		t.Errorf("expected nil,nil; got %v,%v", got, err)
	}
}

func TestSelectFonts_One(t *testing.T) {
	c := []FontCandidate{{FontDir: "", FontName: "Hack-Regular.ttf"}}
	got, err := SelectFonts(c, nil, "")
	if err != nil || len(got) != 1 || got[0].FontName != "Hack-Regular.ttf" {
		t.Errorf("got %v,%v", got, err)
	}
}

func TestSelectFonts_SamePrevKeys(t *testing.T) {
	c := []FontCandidate{{FontName: "Hack-Regular.ttf"}, {FontName: "Hack-Bold.ttf"}}
	got, err := SelectFonts(c, []string{"Hack-Regular.ttf", "Hack-Bold.ttf"}, "")
	if err != nil || len(got) != 2 {
		t.Errorf("expected auto-select all; got %v,%v", got, err)
	}
}

func TestSelectFonts_PromptAll(t *testing.T) {
	fakeStdin(t, "\n")
	c := []FontCandidate{{FontName: "Hack-Regular.ttf"}, {FontName: "Hack-Bold.ttf"}}
	got, err := SelectFonts(c, nil, "")
	if err != nil || len(got) != 2 {
		t.Errorf("expected 2; got %v,%v", got, err)
	}
}

func TestSelectFonts_PromptSkip(t *testing.T) {
	fakeStdin(t, "0\n")
	c := []FontCandidate{{FontName: "Hack-Regular.ttf"}, {FontName: "Hack-Bold.ttf"}}
	_, err := SelectFonts(c, nil, "")
	if err != ErrSkip {
		t.Errorf("expected ErrSkip, got %v", err)
	}
}

func TestSelectFonts_PromptSubset(t *testing.T) {
	fakeStdin(t, "1\n")
	c := []FontCandidate{{FontName: "Hack-Regular.ttf"}, {FontName: "Hack-Bold.ttf"}}
	got, err := SelectFonts(c, nil, "")
	if err != nil || len(got) != 1 || got[0].FontName != "Hack-Regular.ttf" {
		t.Errorf("expected [Hack-Regular.ttf]; got %v,%v", got, err)
	}
}

func TestFontCandidateKey_Root(t *testing.T) {
	c := FontCandidate{FontDir: "", FontName: "Hack.ttf"}
	if got := c.Key(); got != "Hack.ttf" {
		t.Errorf("got %q", got)
	}
}

func TestFontCandidateKey_Subdir(t *testing.T) {
	c := FontCandidate{FontDir: "fonts", FontName: "Hack.ttf"}
	if got := c.Key(); got != "fonts/Hack.ttf" {
		t.Errorf("got %q", got)
	}
}

func TestDeriveFontName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Hack-Regular.ttf", "hack-regular"},
		{"FiraCode Bold.otf", "firacode-bold"},
		{"FONT.TTF", "font"},
		{"no-ext", "no-ext"},
	}
	for _, tc := range cases {
		if got := DeriveFontName(tc.in); got != tc.want {
			t.Errorf("DeriveFontName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPromptFontNames_Empty(t *testing.T) {
	got, err := PromptFontNames(nil, nil, "")
	if err != nil || got != nil {
		t.Errorf("expected nil,nil; got %v,%v", got, err)
	}
}

func TestPromptFontNames_DefaultNames(t *testing.T) {
	fakeStdin(t, "\n")
	c := []FontCandidate{
		{FontDir: "Hack", FontName: "Hack-Regular.ttf"},
		{FontDir: "Hack", FontName: "Hack-Bold.ttf"},
	}
	got, err := PromptFontNames(c, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got["hack-regular"] != "Hack/Hack-Regular.ttf" {
		t.Errorf("unexpected entry for hack-regular: %v", got)
	}
	if got["hack-bold"] != "Hack/Hack-Bold.ttf" {
		t.Errorf("unexpected entry for hack-bold: %v", got)
	}
}

func TestPromptFontNames_Deduplicate(t *testing.T) {
	fakeStdin(t, "\n")
	c := []FontCandidate{
		{FontDir: "a", FontName: "Font.ttf"},
		{FontDir: "b", FontName: "Font.ttf"},
	}
	got, err := PromptFontNames(c, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(got), got)
	}
	if _, ok := got["font"]; !ok {
		t.Errorf("expected key %q, got %v", "font", got)
	}
	if _, ok := got["font-2"]; !ok {
		t.Errorf("expected key %q, got %v", "font-2", got)
	}
}

func TestPromptFontNames_Conflict_Reserved(t *testing.T) {
	fakeStdin(t, "hack-v2\n")
	c := []FontCandidate{{FontDir: "Hack", FontName: "Hack-Regular.ttf"}}
	got, err := PromptFontNames(c, map[string]string{"hack-regular": "other-pkg"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got["hack-v2"] != "Hack/Hack-Regular.ttf" {
		t.Errorf("got %v, want hack-v2 → Hack/Hack-Regular.ttf", got)
	}
}

func TestPromptFontNames_Conflict_Zero_Skips(t *testing.T) {
	fakeStdin(t, "0\n")
	c := []FontCandidate{
		{FontDir: "Hack", FontName: "Hack-Regular.ttf"},
		{FontDir: "Hack", FontName: "Hack-Bold.ttf"},
	}
	_, err := PromptFontNames(c, map[string]string{"hack-regular": "other-pkg"}, "")
	if err != ErrSkip {
		t.Errorf("expected ErrSkip, got %v", err)
	}
}

func TestPromptBinNames_NoConflict_Empty(t *testing.T) {
	fakeStdin(t, "\n")
	result, err := PromptBinNames([]string{"bin/bun"}, []string{"bun"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0] != "bun" {
		t.Errorf("got %v, want [bun]", result)
	}
}

func TestPromptBinNames_NoConflict_InvalidInput_Skips(t *testing.T) {
	fakeStdin(t, "abc\n")
	_, err := PromptBinNames([]string{"bin/bun"}, []string{"bun"}, nil, "")
	if err != ErrSkip {
		t.Errorf("expected ErrSkip for invalid input, got %v", err)
	}
}

func TestPromptBinNames_NoConflict_Zero_Skips(t *testing.T) {
	fakeStdin(t, "0\n")
	_, err := PromptBinNames([]string{"bin/bun"}, []string{"bun"}, nil, "")
	if err != ErrSkip {
		t.Errorf("expected ErrSkip, got %v", err)
	}
}

func TestPromptBinNames_NoConflict_Rename(t *testing.T) {
	fakeStdin(t, "1\nbun-renamed\n")
	result, err := PromptBinNames([]string{"bin/bun"}, []string{"bun"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0] != "bun-renamed" {
		t.Errorf("got %v, want [bun-renamed]", result)
	}
}

func TestPromptBinNames_Conflict_AllConflict_MandatoryRename(t *testing.T) {
	fakeStdin(t, "bun-new\n")
	result, err := PromptBinNames(
		[]string{"bin/bun"},
		[]string{"bun"},
		map[string]string{"bun": "other-pkg"},
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0] != "bun-new" {
		t.Errorf("got %v, want [bun-new]", result)
	}
}

func TestPromptBinNames_Conflict_Duplicate_MandatoryRename(t *testing.T) {
	// item 1 "bun" is first seen → not conflict; item 2 "bun" is duplicate → mandatory rename
	// empty additional prompt → only item 2 renamed
	fakeStdin(t, "\nbun-b\n")
	result, err := PromptBinNames(
		[]string{"bun", "bin/bun"},
		[]string{"bun", "bun"},
		map[string]string{},
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || result[0] != "bun" || result[1] != "bun-b" {
		t.Errorf("got %v, want [bun bun-b]", result)
	}
}

func TestPromptBinNames_Conflict_SomeConflict_ZeroSkips(t *testing.T) {
	fakeStdin(t, "0\n")
	_, err := PromptBinNames(
		[]string{"bin/bun", "bin/extra"},
		[]string{"bun", "extra"},
		map[string]string{"bun": "other-pkg"},
		"",
	)
	if err != ErrSkip {
		t.Errorf("expected ErrSkip, got %v", err)
	}
}

func TestPromptBinNames_Conflict_EmptyAdditional_MandatoryOnly(t *testing.T) {
	fakeStdin(t, "\nbun-new\n")
	result, err := PromptBinNames(
		[]string{"bin/bun", "bin/extra"},
		[]string{"bun", "extra"},
		map[string]string{"bun": "other-pkg"},
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || result[0] != "bun-new" || result[1] != "extra" {
		t.Errorf("got %v, want [bun-new extra]", result)
	}
}

func TestPromptBinNames_Conflict_AdditionalRename(t *testing.T) {
	fakeStdin(t, "2\nbun-new\nextra-new\n")
	result, err := PromptBinNames(
		[]string{"bin/bun", "bin/extra"},
		[]string{"bun", "extra"},
		map[string]string{"bun": "other-pkg"},
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || result[0] != "bun-new" || result[1] != "extra-new" {
		t.Errorf("got %v, want [bun-new extra-new]", result)
	}
}
