package asset

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
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
