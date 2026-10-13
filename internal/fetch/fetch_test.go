package fetch

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

type entry struct {
	name string
	body string
	mode int64
	link bool
}

func tarGz(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: e.mode, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if e.link {
			hdr.Typeflag, hdr.Linkname, hdr.Size = tar.TypeSymlink, e.body, 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if !e.link {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipped(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func write(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

var tree = []entry{
	{name: "go/VERSION", body: "go1.27.2\n", mode: 0644},
	{name: "go/bin/go", body: "binary", mode: 0755},
	{name: "go/src/fmt/print.go", body: "package fmt", mode: 0644},
}

func TestExtractStripsTheTopDirectory(t *testing.T) {
	for name, data := range map[string][]byte{"go.tar.gz": tarGz(t, tree), "go.zip": zipped(t, tree)} {
		dest := filepath.Join(t.TempDir(), "tree")
		if err := Extract(write(t, name, data), dest, 1); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, e := range tree {
			got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(e.name[len("go/"):])))
			if err != nil || string(got) != e.body {
				t.Errorf("%s: %s = %q, %v", name, e.name, got, err)
			}
		}
	}
}

func TestExtractKeepsTheExecutableBit(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "tree")
	if err := Extract(write(t, "go.tar.gz", tarGz(t, tree)), dest, 1); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dest, "bin", "go"))
	if err != nil {
		t.Fatal(err)
	}
	if os.PathSeparator == '/' && info.Mode()&0111 == 0 {
		t.Errorf("bin/go lost its executable bit: %v", info.Mode())
	}
}

func TestExtractRefusesWhatLeavesTheDestination(t *testing.T) {
	escape := []entry{{name: "go/../../outside", body: "x", mode: 0644}}
	link := []entry{{name: "go/link", body: "/etc/passwd", link: true}}
	for name, data := range map[string][]byte{
		"escape.tar.gz": tarGz(t, escape),
		"escape.zip":    zipped(t, escape),
		"link.tar.gz":   tarGz(t, link),
	} {
		parent := t.TempDir()
		if err := Extract(write(t, name, data), filepath.Join(parent, "tree"), 1); err == nil {
			t.Errorf("%s: unpacked without an error", name)
		}
		if _, err := os.Stat(filepath.Join(parent, "outside")); err == nil {
			t.Errorf("%s: wrote outside the destination", name)
		}
	}
	if err := Extract(write(t, "go.7z", []byte("x")), t.TempDir(), 0); err == nil {
		t.Error("an archive of a kind this cannot read unpacked without an error")
	}
}

func TestDownloadChecksTheChecksum(t *testing.T) {
	body := []byte("an archive")
	sum := sha256.Sum256(body)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "a")
	if err := Download(context.Background(), srv.URL+"/a", dest, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); !bytes.Equal(got, body) {
		t.Errorf("downloaded %q", got)
	}

	bad := filepath.Join(t.TempDir(), "b")
	if err := Download(context.Background(), srv.URL+"/b", bad, "00"); err == nil {
		t.Error("a download that does not match its checksum returned no error")
	}
	if _, err := os.Stat(bad); err == nil {
		t.Error("a download that does not match its checksum was left on disk")
	}
	if err := Download(context.Background(), srv.URL+"/missing", bad, "00"); err == nil {
		t.Error("a 404 returned no error")
	}
}
