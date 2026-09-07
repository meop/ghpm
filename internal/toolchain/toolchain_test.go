package toolchain

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeScript(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Installed runs the binary; a shell script stand-in is Unix-only")
	}
	path := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInstalled(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		// gh's real shape: the version is the third word, on the first of two lines.
		{"gh style", `echo 'gh version 2.98.0 (2026-08-20)'; echo 'https://example.com/v2.98.0'`, "2.98.0"},
		// kebab's real shape: the bare version, nothing else.
		{"bare version", `echo '0.1.1'`, "0.1.1"},
		{"v prefixed", `echo 'tool v1.2.3'`, "1.2.3"},
		{"no version printed", `echo 'no idea'`, ""},
		{"prints nothing", `:`, ""},
		{"exits nonzero", `echo '1.0.0'; exit 1`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Installed(writeScript(t, c.body)); got != c.want {
				t.Errorf("Installed() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestInstalled_MissingBinary covers the case that drives a first-time
// vendor: nothing there at all reads as "" rather than blowing up.
func TestInstalled_MissingBinary(t *testing.T) {
	if got := Installed(filepath.Join(t.TempDir(), "nope")); got != "" {
		t.Errorf("Installed() = %q, want %q for a missing binary", got, "")
	}
}
