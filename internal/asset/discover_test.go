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

func TestDiscoverPaths_Root(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, dir, "mytool")
	binDir, name := DiscoverPaths(dir, "mytool")
	if binDir != "" || name != "mytool" {
		t.Errorf("got (%q, %q), want (%q, %q)", binDir, name, "", "mytool")
	}
}

func TestDiscoverPaths_BinSubdir(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, filepath.Join(dir, "bin"), "mytool")
	binDir, name := DiscoverPaths(dir, "mytool")
	if binDir != "bin" || name != "mytool" {
		t.Errorf("got (%q, %q), want (%q, %q)", binDir, name, "bin", "mytool")
	}
}

func TestDiscoverPaths_Subdir(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, filepath.Join(dir, "mytool-1.0"), "mytool")
	binDir, name := DiscoverPaths(dir, "mytool")
	if binDir != "mytool-1.0" || name != "mytool" {
		t.Errorf("got (%q, %q), want (%q, %q)", binDir, name, "mytool-1.0", "mytool")
	}
}

func TestDiscoverPaths_SubdirBin(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, filepath.Join(dir, "mytool-1.0", "bin"), "mytool")
	binDir, name := DiscoverPaths(dir, "mytool")
	if binDir != "mytool-1.0/bin" || name != "mytool" {
		t.Errorf("got (%q, %q), want (%q, %q)", binDir, name, "mytool-1.0/bin", "mytool")
	}
}

func TestDiscoverPaths_NotFound(t *testing.T) {
	dir := t.TempDir()
	binDir, name := DiscoverPaths(dir, "nothere")
	if binDir != "" || name != "" {
		t.Errorf("got (%q, %q), want empty strings", binDir, name)
	}
}
