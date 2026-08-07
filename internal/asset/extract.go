package asset

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bodgit/sevenzip"
	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// dirMode/execMode are both rwxr-xr-x: dirMode for directories created while
// extracting, execMode for a bare binary that has no permission bits of its
// own to preserve (a downloaded release asset, not a tar entry).
const (
	dirMode  os.FileMode = 0755
	execMode os.FileMode = 0755
)

// hasSuffixAny reports whether lower ends with any of suffixes.
func hasSuffixAny(lower string, suffixes ...string) bool {
	for _, s := range suffixes {
		if strings.HasSuffix(lower, s) {
			return true
		}
	}
	return false
}

// classify reports the compression codec ("" for none) an asset's suffix
// implies, and whether it's a tar archive (many files) as opposed to being
// the binary itself once decompressed. The two are independent — a codec can
// show up bare ("tool.gz") or tar-wrapped ("tool.tar.gz"/"tool.tgz") — so one
// pass answers both questions instead of checking suffixes twice.
func classify(lower string) (codec string, isTar bool) {
	switch {
	case hasSuffixAny(lower, ".tar.gz", ".tgz"):
		return "gz", true
	case hasSuffixAny(lower, ".tar.bz2", ".tbz2"):
		return "bz2", true
	case hasSuffixAny(lower, ".tar.xz", ".txz"):
		return "xz", true
	case hasSuffixAny(lower, ".tar.zst", ".tzst"):
		return "zst", true
	case strings.HasSuffix(lower, ".tar"):
		return "", true
	case strings.HasSuffix(lower, ".gz"):
		return "gz", false
	case strings.HasSuffix(lower, ".bz2"):
		return "bz2", false
	case strings.HasSuffix(lower, ".xz"):
		return "xz", false
	case strings.HasSuffix(lower, ".zst"):
		return "zst", false
	}
	return "", false
}

// decompress wraps r for the given codec ("" passes r through unchanged),
// returning a close func for codecs that need one. The one place every
// compression format ghpm supports is implemented, shared by the tar and the
// bare (single-file) extraction path alike.
func decompress(codec string, r io.Reader) (io.Reader, func() error, error) {
	switch codec {
	case "gz":
		gr, err := gzip.NewReader(r)
		if err != nil {
			return nil, nil, err
		}
		return gr, gr.Close, nil
	case "bz2":
		return bzip2.NewReader(r), nil, nil
	case "xz":
		xr, err := xz.NewReader(r)
		if err != nil {
			return nil, nil, fmt.Errorf("xz decompress: %w", err)
		}
		return xr, nil, nil
	case "zst":
		zr, err := zstd.NewReader(r)
		if err != nil {
			return nil, nil, fmt.Errorf("zstd decompress: %w", err)
		}
		return zr, func() error { zr.Close(); return nil }, nil
	default:
		return r, nil, nil
	}
}

// withDecompressedSource opens src, decompresses it per codec, and hands the
// result to consume. Centralizes the open/defer-close/error-wrap boilerplate
// that both the tar path and the bare path in ExtractPackage need.
func withDecompressedSource(src, codec string, consume func(io.Reader) error) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	r, closeFn, err := decompress(codec, f)
	if err != nil {
		return err
	}
	if closeFn != nil {
		defer func() { _ = closeFn() }()
	}
	return consume(r)
}

// stripCodecSuffix removes the trailing ".codec" from name (e.g. "tool.gz",
// "gz" → "tool"), or returns name unchanged when codec is "" (no compression).
func stripCodecSuffix(name, codec string) string {
	if codec == "" {
		return name
	}
	suf := "." + codec
	if strings.HasSuffix(strings.ToLower(name), suf) {
		return name[:len(name)-len(suf)]
	}
	return name
}

// ExtractPackage assumes assetName is already a plain filename, safe to join
// onto a local path with no escape check of its own.
func ExtractPackage(srcDir, assetName, destDir string) error {
	if err := os.MkdirAll(destDir, dirMode); err != nil {
		return err
	}
	src := filepath.Join(srcDir, assetName)
	lower := strings.ToLower(assetName)

	switch {
	case strings.HasSuffix(lower, ".7z"):
		return extract7zPackage(src, destDir)
	case strings.HasSuffix(lower, ".zip"):
		return extractZipPackage(src, destDir)
	}

	codec, isTar := classify(lower)
	if isTar {
		return withDecompressedSource(src, codec, func(r io.Reader) error {
			return extractTarFromReader(r, destDir)
		})
	}
	// Not a container: decompress (if codec != "") and write the result as
	// a single file under its own name minus the codec suffix.
	return withDecompressedSource(src, codec, func(r io.Reader) error {
		target := filepath.Join(destDir, stripCodecSuffix(assetName, codec))
		return streamFile(r, target, execMode)
	})
}

func safeJoin(destDir, name string) (string, error) {
	target := filepath.Join(destDir, name)
	clean := filepath.Clean(target)
	if !withinDir(clean, destDir) {
		return "", fmt.Errorf("path traversal in archive: %s", name)
	}
	return target, nil
}

// withinDir reports whether clean (an already-cleaned path) is destDir itself
// or lives under it.
func withinDir(clean, destDir string) bool {
	cleanDest := filepath.Clean(destDir)
	return clean == cleanDest || strings.HasPrefix(clean, cleanDest+string(os.PathSeparator))
}

// safeSymlinkTarget validates that a tar entry's symlink target can't be used
// to write outside destDir. safeJoin only checks the symlink's own path
// (hdr.Name); nothing constrains hdr.Linkname otherwise, so a crafted archive
// could point a symlink at an arbitrary absolute path, or a relative path that
// escapes destDir via "..", and a later entry (or the extracted content itself)
// that writes through that symlink would land outside the extract dir entirely.
// A relative symlink target is resolved against the symlink's own directory,
// matching normal symlink semantics, not against destDir.
func safeSymlinkTarget(destDir, target, linkname string) error {
	if filepath.IsAbs(linkname) {
		return fmt.Errorf("symlink target is absolute: %s", linkname)
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(target), linkname))
	if !withinDir(resolved, destDir) {
		return fmt.Errorf("symlink target escapes extract dir: %s", linkname)
	}
	return nil
}

func extractTarFromReader(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(destDir, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
				return err
			}
			if err := streamFile(tr, target, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := safeSymlinkTarget(destDir, target, hdr.Linkname); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		}
	}
}

// containerEntry is what extractContainerEntry needs from one archive entry,
// normalized so zip and 7z (whose *File types share this shape but no common
// interface — Name is a field, not a method, on both) can share one
// extraction loop instead of each duplicating it.
type containerEntry struct {
	name string
	mode os.FileMode
	dir  bool
	open func() (io.ReadCloser, error)
}

// extractArchive closes closer once every file has been adapted (the one bit
// of type-specific glue zip/7z still need, since they share no interface)
// and written into destDir.
func extractArchive[F any](destDir string, closer io.Closer, files []F, adapt func(F) containerEntry) error {
	defer func() { _ = closer.Close() }()
	for _, f := range files {
		e := adapt(f)
		if err := extractContainerEntry(destDir, e.name, e.dir, e.mode, e.open); err != nil {
			return err
		}
	}
	return nil
}

func extractContainerEntry(destDir, name string, isDir bool, mode os.FileMode, open func() (io.ReadCloser, error)) error {
	target, err := safeJoin(destDir, name)
	if err != nil {
		return err
	}
	if isDir {
		return os.MkdirAll(target, mode)
	}
	if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
		return err
	}
	rc, err := open()
	if err != nil {
		return err
	}
	err = streamFile(rc, target, mode)
	_ = rc.Close()
	return err
}

// zipEntry maps a *zip.File onto containerEntry — split out from
// extractZipPackage so it's callable on a directly-constructed *zip.File in
// tests, with no real archive needed.
func zipEntry(f *zip.File) containerEntry {
	return containerEntry{f.Name, f.Mode(), f.FileInfo().IsDir(), f.Open}
}

// sevenZipEntry is zipEntry's counterpart for *sevenzip.File.
func sevenZipEntry(f *sevenzip.File) containerEntry {
	return containerEntry{f.Name, f.Mode(), f.FileInfo().IsDir(), f.Open}
}

func extractZipPackage(src, destDir string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	return extractArchive(destDir, zr, zr.File, zipEntry)
}

func extract7zPackage(src, destDir string) error {
	zr, err := sevenzip.OpenReader(src)
	if err != nil {
		return err
	}
	return extractArchive(destDir, zr, zr.File, sevenZipEntry)
}

func streamFile(r io.Reader, path string, mode os.FileMode) error {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, r); err != nil {
		return err
	}
	return out.Sync()
}
