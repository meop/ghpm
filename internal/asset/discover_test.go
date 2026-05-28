package asset

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old })
	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
}

func TestFindBinaries_Root(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, dir, "mytool")
	got := FindBinaries(dir, "mytool")
	if len(got) != 1 || got[0].BinDir != "" || got[0].BinName != "mytool" {
		t.Errorf("got %v, want [{BinDir:%q BinName:%q}]", got, "", "mytool")
	}
}

func TestFindBinaries_BinSubdir(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, filepath.Join(dir, "bin"), "mytool")
	got := FindBinaries(dir, "mytool")
	if len(got) != 1 || got[0].BinDir != "bin" || got[0].BinName != "mytool" {
		t.Errorf("got %v", got)
	}
}

func TestFindBinaries_Subdir(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, filepath.Join(dir, "mytool-1.0"), "mytool")
	got := FindBinaries(dir, "mytool")
	if len(got) != 1 || got[0].BinDir != "mytool-1.0" || got[0].BinName != "mytool" {
		t.Errorf("got %v", got)
	}
}

func TestFindBinaries_SubdirBin(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, filepath.Join(dir, "mytool-1.0", "bin"), "mytool")
	got := FindBinaries(dir, "mytool")
	if len(got) != 1 || got[0].BinDir != "mytool-1.0/bin" || got[0].BinName != "mytool" {
		t.Errorf("got %v", got)
	}
}

func TestFindBinaries_NotFound(t *testing.T) {
	dir := t.TempDir()
	got := FindBinaries(dir, "nothere")
	if len(got) != 0 {
		t.Errorf("expected no results, got %v", got)
	}
}

func TestFindBinaries_Multiple(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, dir, "mytool")
	writeFakeBinary(t, dir, "mytool-extra")
	got := FindBinaries(dir, "mytool")
	if len(got) != 2 {
		t.Errorf("expected 2 results, got %d: %v", len(got), got)
	}
}

func TestSelectBinaries_None(t *testing.T) {
	got, err := SelectBinaries(nil, nil)
	if got != nil || err != nil {
		t.Errorf("expected nil,nil; got %v,%v", got, err)
	}
}

func TestSelectBinaries_One(t *testing.T) {
	c := []BinaryCandidate{{BinDir: "", BinName: "tool"}}
	got, err := SelectBinaries(c, nil)
	if err != nil || len(got) != 1 || got[0].BinName != "tool" {
		t.Errorf("got %v,%v", got, err)
	}
}

func TestSelectBinaries_SamePrevNames(t *testing.T) {
	c := []BinaryCandidate{{BinName: "uv"}, {BinName: "uvx"}}
	got, err := SelectBinaries(c, []string{"uv", "uvx"})
	if err != nil || len(got) != 2 {
		t.Errorf("expected auto-select all; got %v,%v", got, err)
	}
}

func TestSelectBinaries_PromptAll(t *testing.T) {
	fakeStdin(t, "\n")
	c := []BinaryCandidate{{BinName: "uv"}, {BinName: "uvx"}}
	got, err := SelectBinaries(c, nil)
	if err != nil || len(got) != 2 {
		t.Errorf("expected 2; got %v,%v", got, err)
	}
}

func TestSelectBinaries_PromptSkip(t *testing.T) {
	fakeStdin(t, "0\n")
	c := []BinaryCandidate{{BinName: "uv"}, {BinName: "uvx"}}
	_, err := SelectBinaries(c, nil)
	if err != ErrSkip {
		t.Errorf("expected ErrSkip, got %v", err)
	}
}

func TestSelectBinaries_PromptSubset(t *testing.T) {
	fakeStdin(t, "1\n")
	c := []BinaryCandidate{{BinName: "uv"}, {BinName: "uvx"}}
	got, err := SelectBinaries(c, nil)
	if err != nil || len(got) != 1 || got[0].BinName != "uv" {
		t.Errorf("expected [uv]; got %v,%v", got, err)
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
	got, err := SelectFonts(nil, nil)
	if got != nil || err != nil {
		t.Errorf("expected nil,nil; got %v,%v", got, err)
	}
}

func TestSelectFonts_One(t *testing.T) {
	c := []FontCandidate{{FontDir: "", FontName: "Hack-Regular.ttf"}}
	got, err := SelectFonts(c, nil)
	if err != nil || len(got) != 1 || got[0].FontName != "Hack-Regular.ttf" {
		t.Errorf("got %v,%v", got, err)
	}
}

func TestSelectFonts_SamePrevKeys(t *testing.T) {
	c := []FontCandidate{{FontName: "Hack-Regular.ttf"}, {FontName: "Hack-Bold.ttf"}}
	got, err := SelectFonts(c, []string{"Hack-Regular.ttf", "Hack-Bold.ttf"})
	if err != nil || len(got) != 2 {
		t.Errorf("expected auto-select all; got %v,%v", got, err)
	}
}

func TestSelectFonts_PromptAll(t *testing.T) {
	fakeStdin(t, "\n")
	c := []FontCandidate{{FontName: "Hack-Regular.ttf"}, {FontName: "Hack-Bold.ttf"}}
	got, err := SelectFonts(c, nil)
	if err != nil || len(got) != 2 {
		t.Errorf("expected 2; got %v,%v", got, err)
	}
}

func TestSelectFonts_PromptSkip(t *testing.T) {
	fakeStdin(t, "0\n")
	c := []FontCandidate{{FontName: "Hack-Regular.ttf"}, {FontName: "Hack-Bold.ttf"}}
	_, err := SelectFonts(c, nil)
	if err != ErrSkip {
		t.Errorf("expected ErrSkip, got %v", err)
	}
}

func TestSelectFonts_PromptSubset(t *testing.T) {
	fakeStdin(t, "1\n")
	c := []FontCandidate{{FontName: "Hack-Regular.ttf"}, {FontName: "Hack-Bold.ttf"}}
	got, err := SelectFonts(c, nil)
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
	got := PromptFontNames(nil)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestPromptFontNames_DefaultNames(t *testing.T) {
	fakeStdin(t, "\n") // accept defaults
	c := []FontCandidate{
		{FontDir: "Hack", FontName: "Hack-Regular.ttf"},
		{FontDir: "Hack", FontName: "Hack-Bold.ttf"},
	}
	got := PromptFontNames(c)
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
	got := PromptFontNames(c)
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
