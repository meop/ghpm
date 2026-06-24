package asset

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestSafeJoin_RejectsTraversal(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"parent", "../etc/passwd"},
		{"deep parent", "foo/../../etc/passwd"},
	}
	for _, c := range cases {
		_, err := safeJoin("/tmp/dest", c.path)
		if err == nil {
			t.Errorf("safeJoin(%q): expected error, got nil", c.path)
		}
	}
}

func TestSafeJoin_AcceptsValid(t *testing.T) {
	cases := []struct {
		input  string
		expect string
	}{
		{"foo.txt", "/tmp/dest/foo.txt"},
		{"sub/foo.txt", "/tmp/dest/sub/foo.txt"},
		{"a/b/c/d", "/tmp/dest/a/b/c/d"},
	}
	for _, c := range cases {
		got, err := safeJoin("/tmp/dest", c.input)
		if err != nil {
			t.Errorf("safeJoin(%q): unexpected error: %v", c.input, err)
			continue
		}
		want := filepath.Clean(c.expect)
		if filepath.Clean(got) != want {
			t.Errorf("safeJoin(%q) = %q, want %q", c.input, got, want)
		}
	}
}

// writeTarGz creates srcDir/name as a .tar.gz holding the given path→content files.
func writeTarGz(t *testing.T, srcDir, name string, files map[string]string) {
	t.Helper()
	f, err := os.Create(filepath.Join(srcDir, name))
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)
	for path, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: path, Typeflag: tar.TypeReg, Mode: 0755, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []io.Closer{tw, gw, f} {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// TestExtractPackage_OverlayLastWins documents the multi-asset overlay guarantee:
// extracting several assets into one dir in order merges their trees, and a later
// asset overwrites a colliding path ("last wins"), while non-colliding files from
// each asset coexist.
func TestExtractPackage_OverlayLastWins(t *testing.T) {
	src := t.TempDir()
	dest := filepath.Join(t.TempDir(), "overlay")

	writeTarGz(t, src, "main.tar.gz", map[string]string{
		"bin/llama-server": "server",
		"bin/ggml.so":      "ggml-v1",
	})
	writeTarGz(t, src, "cudart.tar.gz", map[string]string{
		"bin/ggml.so":     "ggml-v2",
		"bin/cudart64.so": "cudart",
	})

	for _, name := range []string{"main.tar.gz", "cudart.tar.gz"} {
		if err := ExtractPackage(src, name, dest); err != nil {
			t.Fatalf("extract %s: %v", name, err)
		}
	}

	want := map[string]string{
		"bin/llama-server": "server",  // only in main
		"bin/cudart64.so":  "cudart",  // only in cudart
		"bin/ggml.so":      "ggml-v2", // in both; later asset (cudart) wins
	}
	for path, content := range want {
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(path)))
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if string(got) != content {
			t.Errorf("%s = %q, want %q", path, got, content)
		}
	}
}

func TestExtractPackage_TarSlipRejected(t *testing.T) {
	dest := t.TempDir()

	tarPath := filepath.Join(dest, "evil.tar.gz")
	f, err := os.Create(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)
	if err := tw.WriteHeader(&tar.Header{Name: "../escaped.txt", Typeflag: tar.TypeReg, Size: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	err = ExtractPackage(dest, "evil.tar.gz", filepath.Join(dest, "out"))
	if err == nil {
		t.Fatal("expected path traversal error")
	}
}

func TestExtractPackage_ZipSlipRejected(t *testing.T) {
	dest := t.TempDir()

	zipPath := filepath.Join(dest, "evil.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("../escaped.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	err = ExtractPackage(dest, "evil.zip", filepath.Join(dest, "out"))
	if err == nil {
		t.Fatal("expected path traversal error")
	}
}
