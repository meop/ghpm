package asset

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// Extract extracts the binary from the downloaded asset in srcDir and copies it
// to destDir with name outputName. hint is the stored binary_name from a previous
// install (used to prefer the same binary on update; pass "" for fresh installs).
// Returns the base name of the binary as found inside the archive.
func Extract(srcDir, assetName, destDir, outputName, hint string) (string, error) {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", err
	}

	src := filepath.Join(srcDir, assetName)
	lower := strings.ToLower(assetName)

	var binaryName, binPath string
	var err error

	switch {
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		binaryName, binPath, err = extractTar(src, destDir, outputName, hint, "gz")
	case strings.HasSuffix(lower, ".tar.bz2"):
		binaryName, binPath, err = extractTar(src, destDir, outputName, hint, "bz2")
	case strings.HasSuffix(lower, ".tar.xz"):
		binaryName, binPath, err = extractTarXZ(src, destDir, outputName, hint)
	case strings.HasSuffix(lower, ".zip"):
		binaryName, binPath, err = extractZip(src, destDir, outputName, hint)
	default:
		binaryName, binPath, err = copyRaw(src, destDir, outputName)
	}
	if err != nil {
		return "", err
	}
	if binPath == "" {
		return "", fmt.Errorf("no binary found in %s", assetName)
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		return "", err
	}
	return binaryName, nil
}

func extractTar(src, destDir, outputName, hint, compression string) (string, string, error) {
	f, err := os.Open(src)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = f.Close() }()

	var r io.Reader
	switch compression {
	case "gz":
		gr, err := gzip.NewReader(f)
		if err != nil {
			return "", "", err
		}
		defer func() { _ = gr.Close() }()
		r = gr
	case "bz2":
		r = bzip2.NewReader(f)
	default:
		r = f
	}

	return extractTarReader(r, destDir, outputName, hint)
}

func extractTarXZ(src, destDir, outputName, hint string) (string, string, error) {
	tmp := filepath.Join(destDir, "_tarxz_extract")
	if err := os.MkdirAll(tmp, 0755); err != nil {
		return "", "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	cmd := exec.Command("tar", "xJf", src, "-C", tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("tar xJf: %s", out)
	}
	return findAndMoveBinary(tmp, destDir, outputName, hint)
}

func extractTarReader(r io.Reader, destDir, outputName, hint string) (string, string, error) {
	tmp := filepath.Join(destDir, "_tar_extract")
	if err := os.MkdirAll(tmp, 0755); err != nil {
		return "", "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", "", err
		}
		if hdr.Typeflag == tar.TypeReg {
			base := filepath.Base(hdr.Name)
			out := filepath.Join(tmp, base)
			if err := writeFile(tr, out, os.FileMode(hdr.Mode)); err != nil {
				return "", "", err
			}
		}
	}
	return findAndMoveBinary(tmp, destDir, outputName, hint)
}

func extractZip(src, destDir, outputName, hint string) (string, string, error) {
	tmp := filepath.Join(destDir, "_zip_extract")
	if err := os.MkdirAll(tmp, 0755); err != nil {
		return "", "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	zr, err := zip.OpenReader(src)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = zr.Close() }()

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		base := filepath.Base(f.Name)
		out := filepath.Join(tmp, base)
		rc, err := f.Open()
		if err != nil {
			return "", "", err
		}
		if err := writeFile(rc, out, f.Mode()); err != nil {
			_ = rc.Close()
			return "", "", err
		}
		_ = rc.Close()
	}
	return findAndMoveBinary(tmp, destDir, outputName, hint)
}

func copyRaw(src, destDir, outputName string) (string, string, error) {
	if runtime.GOOS == "windows" && !strings.HasSuffix(outputName, ".exe") {
		outputName += ".exe"
	}
	dest := filepath.Join(destDir, outputName)
	data, err := os.ReadFile(src)
	if err != nil {
		return "", "", err
	}
	return outputName, dest, os.WriteFile(dest, data, 0755)
}

func writeFile(r io.Reader, path string, mode os.FileMode) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}

// findAndMoveBinary picks the best binary from tmp (a flat extract dir),
// moves it to destDir/outputName, and returns (archiveBinaryName, destPath, err).
// hint is the stored binary_name from a previous install; it takes priority over outputName
// for identifying the right file in the archive.
func findAndMoveBinary(tmp, destDir, outputName, hint string) (string, string, error) {
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return "", "", err
	}

	// Collect executable candidates
	var candidates []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if isExecutable(info) {
			candidates = append(candidates, filepath.Join(tmp, e.Name()))
		}
	}
	// Fallback: any file if no executables found
	if len(candidates) == 0 {
		for _, e := range entries {
			if !e.IsDir() {
				candidates = append(candidates, filepath.Join(tmp, e.Name()))
			}
		}
	}
	if len(candidates) == 0 {
		return "", "", fmt.Errorf("no files found in archive")
	}

	// Try hint first (stored binary_name from previous install)
	if hint != "" {
		for _, c := range candidates {
			base := strings.TrimSuffix(filepath.Base(c), ".exe")
			if strings.EqualFold(base, hint) {
				return moveChosen(c, destDir, outputName)
			}
		}
	}

	// Try exact match on outputName (package name)
	for _, c := range candidates {
		base := strings.TrimSuffix(filepath.Base(c), ".exe")
		if strings.EqualFold(base, outputName) {
			return moveChosen(c, destDir, outputName)
		}
	}

	// Single candidate — auto-select
	if len(candidates) == 1 {
		return moveChosen(candidates[0], destDir, outputName)
	}

	// Multiple candidates — prompt user
	fmt.Printf("choose which binary to use for %q:\n", outputName)
	for i, c := range candidates {
		fmt.Printf("  %d) %s\n", i+1, filepath.Base(c))
	}
	fmt.Print("Enter number: ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(line)
	var idx int
	if _, err := fmt.Sscanf(line, "%d", &idx); err != nil || idx < 1 || idx > len(candidates) {
		return "", "", fmt.Errorf("invalid selection")
	}
	return moveChosen(candidates[idx-1], destDir, outputName)
}

// moveChosen copies chosen to destDir/outputName and returns (archiveBinaryName, destPath, err).
func moveChosen(chosen, destDir, outputName string) (string, string, error) {
	archiveName := strings.TrimSuffix(filepath.Base(chosen), ".exe")
	dest := filepath.Join(destDir, outputName)
	if runtime.GOOS == "windows" && !strings.HasSuffix(dest, ".exe") {
		dest += ".exe"
	}
	data, err := os.ReadFile(chosen)
	if err != nil {
		return "", "", err
	}
	return archiveName, dest, os.WriteFile(dest, data, 0755)
}

func isExecutable(info os.FileInfo) bool {
	return info.Mode()&0111 != 0
}
