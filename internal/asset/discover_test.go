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
	w.Close()
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
	got, err := SelectBinaries(nil, "x", nil)
	if got != nil || err != nil {
		t.Errorf("expected nil,nil; got %v,%v", got, err)
	}
}

func TestSelectBinaries_One(t *testing.T) {
	c := []BinaryCandidate{{BinDir: "", BinName: "tool"}}
	got, err := SelectBinaries(c, "tool", nil)
	if err != nil || len(got) != 1 || got[0].BinName != "tool" {
		t.Errorf("got %v,%v", got, err)
	}
}

func TestSelectBinaries_SamePrevNames(t *testing.T) {
	c := []BinaryCandidate{{BinName: "uv"}, {BinName: "uvx"}}
	got, err := SelectBinaries(c, "uv", []string{"uv", "uvx"})
	if err != nil || len(got) != 2 {
		t.Errorf("expected auto-select all; got %v,%v", got, err)
	}
}

func TestSelectBinaries_PromptAll(t *testing.T) {
	fakeStdin(t, "\n")
	c := []BinaryCandidate{{BinName: "uv"}, {BinName: "uvx"}}
	got, err := SelectBinaries(c, "uv", nil)
	if err != nil || len(got) != 2 {
		t.Errorf("expected 2; got %v,%v", got, err)
	}
}

func TestSelectBinaries_PromptSkip(t *testing.T) {
	fakeStdin(t, "0\n")
	c := []BinaryCandidate{{BinName: "uv"}, {BinName: "uvx"}}
	_, err := SelectBinaries(c, "uv", nil)
	if err != ErrSkip {
		t.Errorf("expected ErrSkip, got %v", err)
	}
}

func TestSelectBinaries_PromptSubset(t *testing.T) {
	fakeStdin(t, "1\n")
	c := []BinaryCandidate{{BinName: "uv"}, {BinName: "uvx"}}
	got, err := SelectBinaries(c, "uv", nil)
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
