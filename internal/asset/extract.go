package asset

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
		return copyRawPackage(src, destDir, assetName)
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

	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, hdr.Name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			if err := writeFile(tr, target, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeSymlink:
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

func extractTarXZPackage(src, destDir string) error {
	cmd := exec.Command("tar", "xJf", src, "-C", destDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tar xJf: %s", out)
	}
	return nil
}

func extractZipPackage(src, destDir string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer func() { _ = zr.Close() }()

	for _, f := range zr.File {
		target := filepath.Join(destDir, f.Name)
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
		err = writeFile(rc, target, f.Mode())
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func copyRawPackage(src, destDir, name string) error {
	dest := filepath.Join(destDir, filepath.Base(name))
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0755)
}

func writeFile(r io.Reader, path string, mode os.FileMode) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}
