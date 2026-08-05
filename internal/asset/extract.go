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

	"github.com/ulikunitz/xz"
)

func ExtractPackage(srcDir, assetName, destDir string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	src := filepath.Join(srcDir, assetName)
	lower := strings.ToLower(assetName)
	switch {
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return extractTarPackage(src, destDir, "gz")
	case strings.HasSuffix(lower, ".tar.bz2"):
		return extractTarPackage(src, destDir, "bz2")
	case strings.HasSuffix(lower, ".tar.xz"):
		return extractTarXZPackage(src, destDir)
	case strings.HasSuffix(lower, ".zip"):
		return extractZipPackage(src, destDir)
	default:
		// Unreachable via the normal selection flow: isSkipped requires one of
		// the extensions above to consider an asset a candidate at all, so
		// nothing without one ever reaches here as a chosen asset. Kept as an
		// explicit error rather than a silent raw-file copy (the previous
		// behavior) since that fallback was dead code with no caller, no test,
		// and no documented feature it was meant to support.
		return fmt.Errorf("unsupported asset type: %s", assetName)
	}
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
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			if err := streamFile(tr, target, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := safeSymlinkTarget(destDir, target, hdr.Linkname); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		}
	}
}

func extractTarPackage(src, destDir, compression string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	var r io.Reader
	switch compression {
	case "gz":
		gr, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer func() { _ = gr.Close() }()
		r = gr
	case "bz2":
		r = bzip2.NewReader(f)
	default:
		r = f
	}

	return extractTarFromReader(r, destDir)
}

func extractTarXZPackage(src, destDir string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	xr, err := xz.NewReader(f)
	if err != nil {
		return fmt.Errorf("xz decompress: %w", err)
	}

	return extractTarFromReader(xr, destDir)
}

func extractZipPackage(src, destDir string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer func() { _ = zr.Close() }()

	for _, f := range zr.File {
		target, err := safeJoin(destDir, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, f.Mode()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = streamFile(rc, target, f.Mode())
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
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
