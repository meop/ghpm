// Package fetch fetches an archive, checks it, and unpacks it. Everything is
// the standard library's: there is no curl, tar or unzip to find on the host.
package fetch

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var client = &http.Client{}

// Download fetches url into dest and returns an error unless what arrived has
// the sha256 wantSHA256. The hash is taken as the bytes are written, and a
// file that does not match is removed rather than left to be unpacked.
func Download(ctx context.Context, url, dest, wantSHA256 string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(f, h), resp.Body)
	closeErr := f.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("%s: %w", url, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, wantSHA256) {
		_ = os.Remove(dest)
		return fmt.Errorf("%s does not match its checksum: got %s, want %s", filepath.Base(dest), got, wantSHA256)
	}
	return nil
}

// Extract unpacks a .tar.gz or .zip archive into destDir, dropping the first
// strip leading path elements of every entry. An entry that would land
// outside destDir fails the whole extraction.
func Extract(archive, destDir string, strip int) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	lower := strings.ToLower(archive)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(archive, destDir, strip)
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(archive, destDir, strip)
	}
	return fmt.Errorf("%s: not a .tar.gz or .zip archive", filepath.Base(archive))
}

// target maps an archive entry to its path under destDir, or "" for an entry
// the strip consumes entirely.
func target(destDir, name string, strip int) (string, error) {
	parts := strings.Split(strings.Trim(filepath.ToSlash(name), "/"), "/")
	if len(parts) <= strip {
		return "", nil
	}
	rel := filepath.Join(parts[strip:]...)
	if !filepath.IsLocal(rel) {
		return "", fmt.Errorf("archive entry %q leaves the destination", name)
	}
	return filepath.Join(destDir, rel), nil
}

func writeFile(path string, mode os.FileMode, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm()|0200)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, r) //nolint:gosec // the archive is checksummed before it is unpacked
	return errors.Join(copyErr, f.Close())
}

func extractTarGz(archive, destDir string, strip int) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		path, err := target(destDir, hdr.Name, strip)
		if err != nil {
			return err
		}
		if path == "" {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeFile(path, hdr.FileInfo().Mode(), tr); err != nil {
				return err
			}
		case tar.TypeXGlobalHeader, tar.TypeXHeader:
		default:
			// go's archives are plain files and directories; a link would be
			// something this was not written to place safely
			return fmt.Errorf("archive entry %q has an unsupported type", hdr.Name)
		}
	}
}

func extractZip(archive, destDir string, strip int) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer func() { _ = zr.Close() }()
	for _, zf := range zr.File {
		path, err := target(destDir, zf.Name, strip)
		if err != nil {
			return err
		}
		if path == "" {
			continue
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0755); err != nil {
				return err
			}
			continue
		}
		if !zf.FileInfo().Mode().IsRegular() {
			return fmt.Errorf("archive entry %q has an unsupported type", zf.Name)
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		err = writeFile(path, zf.FileInfo().Mode(), rc)
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
