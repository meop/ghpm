package asset

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bodgit/sevenzip"
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

// writeTarEntries writes path→content files as tar entries to tw.
func writeTarEntries(t *testing.T, tw *tar.Writer, files map[string]string) {
	t.Helper()
	for path, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: path, Typeflag: tar.TypeReg, Mode: 0755, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
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
	writeTarEntries(t, tw, files)
	for _, c := range []io.Closer{tw, gw, f} {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// TestClassify covers ghpm's own suffix→(codec, isTar) decision — including
// the alias/canonical pairs (tgz vs tar.gz, etc.) — without needing any real
// compressed bytes. Whether a given codec's library actually decodes
// correctly is that library's own concern, not ghpm's to re-verify.
func TestClassify(t *testing.T) {
	cases := []struct {
		name      string
		wantCodec string
		wantTar   bool
	}{
		{"tool.tar.gz", "gz", true},
		{"tool.tgz", "gz", true},
		{"tool.tar.bz2", "bz2", true},
		{"tool.tbz2", "bz2", true},
		{"tool.tar.xz", "xz", true},
		{"tool.txz", "xz", true},
		{"tool.tar.zst", "zst", true},
		{"tool.tzst", "zst", true},
		{"tool.tar", "", true},
		{"tool.gz", "gz", false},
		{"tool.bz2", "bz2", false},
		{"tool.xz", "xz", false},
		{"tool.zst", "zst", false},
		{"tool", "", false},
		{"tool.exe", "", false},
	}
	for _, c := range cases {
		codec, isTar := classify(c.name)
		if codec != c.wantCodec || isTar != c.wantTar {
			t.Errorf("classify(%q) = (%q, %v), want (%q, %v)", c.name, codec, isTar, c.wantCodec, c.wantTar)
		}
	}
}

func TestStripCodecSuffix(t *testing.T) {
	cases := []struct {
		name  string
		codec string
		want  string
	}{
		{"tool.gz", "gz", "tool"},
		{"tool.tar.gz", "gz", "tool.tar"},
		{"tool", "", "tool"},
		{"tool.gz", "", "tool.gz"},
		{"tool.gz", "bz2", "tool.gz"},
	}
	for _, c := range cases {
		if got := stripCodecSuffix(c.name, c.codec); got != c.want {
			t.Errorf("stripCodecSuffix(%q, %q) = %q, want %q", c.name, c.codec, got, c.want)
		}
	}
}

func TestExtractPackage_BareTarRoundTrip(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	f, err := os.Create(filepath.Join(src, "tool.tar"))
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	writeTarEntries(t, tw, map[string]string{"bin/tool": "hello"})
	for _, c := range []io.Closer{tw, f} {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}

	if err := ExtractPackage(src, "tool.tar", dest); err != nil {
		t.Fatalf("extract tool.tar: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want %q", got, "hello")
	}
}

// TestExtractPackage_BareBinary covers an asset that isn't an archive at
// all — shfmt and jq ship these for real (checked against the actual releases).
func TestExtractPackage_BareBinary(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	name := "shfmt_v3.13.1_linux_amd64"
	content := "raw-binary-bytes"
	if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ExtractPackage(src, name, dest); err != nil {
		t.Fatalf("extract %s: %v", name, err)
	}
	got, err := os.ReadFile(filepath.Join(dest, name))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Errorf("content = %q, want %q", got, content)
	}
}

// TestExtractPackage_UnrecognizedSuffixCopiedRaw covers suffixes classify
// doesn't recognize (including formats ghpm has no decoder for, like legacy
// Unix "compress" or lzip): same as a genuinely bare binary, the bytes are
// copied through unchanged rather than guessed at or rejected here.
func TestExtractPackage_UnrecognizedSuffixCopiedRaw(t *testing.T) {
	for _, name := range []string{"tool-linux-amd64.Z", "tool-linux-amd64.lz"} {
		src := t.TempDir()
		dest := t.TempDir()
		if err := os.WriteFile(filepath.Join(src, name), []byte("compressed-bytes"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := ExtractPackage(src, name, dest); err != nil {
			t.Fatalf("ExtractPackage(%q): unexpected error: %v", name, err)
		}
		got, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "compressed-bytes" {
			t.Errorf("content = %q, want unchanged %q", got, "compressed-bytes")
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

// fakeEntry and its adapt closure stand in for *zip.File/*sevenzip.File in
// the tests below — extractArchive is generic over the entry type, so the
// shared logic it and extractContainerEntry do (path safety, dir creation,
// content streaming, closing) can be verified without a real zip or 7z file
// at all. The per-library adapters themselves are a single trivial, type-checked
// struct literal each (see extractZipPackage/extract7zPackage) and aren't
// separately tested.
type fakeEntry struct {
	name    string
	isDir   bool
	content string
}

func fakeAdapt(f fakeEntry) containerEntry {
	mode := os.FileMode(0644)
	if f.isDir {
		mode = 0755
	}
	return containerEntry{
		name: f.name,
		mode: mode,
		dir:  f.isDir,
		open: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(f.content)), nil
		},
	}
}

type fakeCloser struct{ closed *bool }

func (c fakeCloser) Close() error {
	*c.closed = true
	return nil
}

func TestExtractArchive(t *testing.T) {
	dest := t.TempDir()
	entries := []fakeEntry{
		{name: "sub", isDir: true},
		{name: "sub/file.txt", content: "hello"},
		{name: "root.txt", content: "world"},
	}
	if err := extractArchive(dest, io.NopCloser(nil), entries, fakeAdapt); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dest, "sub", "file.txt")); err != nil || string(got) != "hello" {
		t.Errorf("sub/file.txt = %q, %v; want %q, nil", got, err, "hello")
	}
	if got, err := os.ReadFile(filepath.Join(dest, "root.txt")); err != nil || string(got) != "world" {
		t.Errorf("root.txt = %q, %v; want %q, nil", got, err, "world")
	}
}

func TestExtractArchive_RejectsTraversal(t *testing.T) {
	dest := t.TempDir()
	entries := []fakeEntry{{name: "../escaped.txt", content: "evil"}}
	if err := extractArchive(dest, io.NopCloser(nil), entries, fakeAdapt); err == nil {
		t.Fatal("expected path traversal error")
	}
}

func TestExtractArchive_ClosesCloser(t *testing.T) {
	closed := false
	_ = extractArchive(t.TempDir(), fakeCloser{&closed}, nil, fakeAdapt)
	if !closed {
		t.Error("expected closer to be closed")
	}
}

// TestZipEntry/TestSevenZipEntry cover the one bit of real-library glue
// zip/7z each still need: no real archive is built or read — *zip.File and
// *sevenzip.File are plain structs, so a FileHeader with just the fields we
// care about set is enough to call the adapter directly.
func TestZipEntry(t *testing.T) {
	fh := zip.FileHeader{Name: "bin/tool"}
	fh.SetMode(0644)
	e := zipEntry(&zip.File{FileHeader: fh})
	if e.name != "bin/tool" {
		t.Errorf("name = %q, want %q", e.name, "bin/tool")
	}
	if e.dir {
		t.Error("expected dir = false for a regular file")
	}
}

func TestZipEntry_Dir(t *testing.T) {
	fh := zip.FileHeader{Name: "sub/"}
	fh.SetMode(os.ModeDir | 0755)
	e := zipEntry(&zip.File{FileHeader: fh})
	if !e.dir {
		t.Error("expected dir = true")
	}
}

func TestSevenZipEntry(t *testing.T) {
	e := sevenZipEntry(&sevenzip.File{Name: "bin/tool"})
	if e.name != "bin/tool" {
		t.Errorf("name = %q, want %q", e.name, "bin/tool")
	}
	if e.dir {
		t.Error("expected dir = false for a regular file (zero-value Attributes)")
	}
}

func TestSevenZipEntry_Dir(t *testing.T) {
	// 0x10 is sevenzip's internal MSDOS "directory" attribute bit (not
	// exported by the library; confirmed by reading its source).
	fh := sevenzip.FileHeader{Name: "sub/", Attributes: 0x10}
	e := sevenZipEntry(&sevenzip.File{FileHeader: fh})
	if !e.dir {
		t.Error("expected dir = true")
	}
}

// writeTarGzSymlink creates srcDir/name as a .tar.gz holding a single symlink
// entry named linkName pointing at target.
func writeTarGzSymlink(t *testing.T, srcDir, name, linkName, target string) {
	t.Helper()
	f, err := os.Create(filepath.Join(srcDir, name))
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)
	if err := tw.WriteHeader(&tar.Header{Name: linkName, Typeflag: tar.TypeSymlink, Linkname: target, Mode: 0777}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []io.Closer{tw, gw, f} {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// TestExtractPackage_TarSymlinkAbsoluteTargetRejected is the regression test
// for a real gap: safeJoin only validated a symlink entry's own path, never
// where it points. An absolute Linkname let a crafted tarball plant a symlink
// pointing anywhere on disk (e.g. the user's ~/.bashrc); anything that later
// wrote through that symlink name would land outside the extract dir entirely.
func TestExtractPackage_TarSymlinkAbsoluteTargetRejected(t *testing.T) {
	dest := t.TempDir()
	writeTarGzSymlink(t, dest, "evil.tar.gz", "evil-link", filepath.Join(dest, "outside-target"))

	err := ExtractPackage(dest, "evil.tar.gz", filepath.Join(dest, "out"))
	if err == nil {
		t.Fatal("expected symlink target to be rejected as absolute")
	}
}

// TestExtractPackage_TarSymlinkRelativeEscapeRejected covers the relative-path
// variant: a Linkname using ".." to climb out of the extract dir even though
// the symlink's own name is safely inside it.
func TestExtractPackage_TarSymlinkRelativeEscapeRejected(t *testing.T) {
	dest := t.TempDir()
	writeTarGzSymlink(t, dest, "evil.tar.gz", "sub/evil-link", "../../../escaped")

	err := ExtractPackage(dest, "evil.tar.gz", filepath.Join(dest, "out"))
	if err == nil {
		t.Fatal("expected symlink target to be rejected as an escape")
	}
}

// TestExtractPackage_TarSymlinkWithinDirAllowed confirms the fix isn't
// over-broad: a normal relative symlink that stays inside the extract dir
// (e.g. "libfoo.so -> libfoo.so.1", a common pattern in release tarballs)
// still extracts successfully.
func TestExtractPackage_TarSymlinkWithinDirAllowed(t *testing.T) {
	dest := t.TempDir()
	out := filepath.Join(dest, "out")
	writeTarGzSymlink(t, dest, "ok.tar.gz", "lib/libfoo.so", "libfoo.so.1")

	if err := ExtractPackage(dest, "ok.tar.gz", out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := os.Readlink(filepath.Join(out, "lib", "libfoo.so"))
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if got != "libfoo.so.1" {
		t.Errorf("symlink target = %q, want %q", got, "libfoo.so.1")
	}
}
