package ioutils_test

import (
	"bufio"
	"errors"
	"os"
	"testing"

	"github.com/meop/ghpm/internal/ioutils"
)

func setStdin(t *testing.T, input string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := ioutils.Stdin
	ioutils.Stdin = bufio.NewReader(r)
	t.Cleanup(func() { ioutils.Stdin = old })
	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
}

func TestReadSingle_Valid(t *testing.T) {
	setStdin(t, "2\n")
	idx, err := ioutils.ReadSingle("pick one")
	if err != nil || idx != 2 {
		t.Errorf("got %d, %v; want 2, nil", idx, err)
	}
}

func TestReadSingle_Zero_Skips(t *testing.T) {
	setStdin(t, "0\n")
	_, err := ioutils.ReadSingle("pick one")
	if !errors.Is(err, ioutils.ErrSkip) {
		t.Errorf("expected ErrSkip, got %v", err)
	}
}

func TestReadSingle_Invalid(t *testing.T) {
	setStdin(t, "abc\n")
	_, err := ioutils.ReadSingle("pick one")
	if !errors.Is(err, ioutils.ErrSkip) {
		t.Errorf("expected ErrSkip for invalid input, got %v", err)
	}
}

func TestReadSingle_Empty(t *testing.T) {
	setStdin(t, "\n")
	_, err := ioutils.ReadSingle("pick one")
	if !errors.Is(err, ioutils.ErrSkip) {
		t.Errorf("expected ErrSkip for empty input, got %v", err)
	}
}
