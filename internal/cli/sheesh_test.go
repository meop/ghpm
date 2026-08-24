package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/store"
)

// sheeshArchTag maps runtime.GOARCH to the Rust-style token sheesh's own
// release assets use (see install.sh's ARCH normalization).
func sheeshArchTag() string {
	switch runtime.GOARCH {
	case "arm64":
		return "aarch64"
	case "amd64":
		return "x86_64"
	default:
		return ""
	}
}

// fakeSheeshAsset builds a release asset for the current platform in
// sheesh's own naming convention (sheesh-<ver>-<os>-<arch>.<ext>),
// containing a single executable file named "kebab".
func fakeSheeshAsset(t *testing.T) (assetName string, content []byte) {
	t.Helper()
	arch := sheeshArchTag()
	if arch == "" {
		t.Skipf("no sheesh release convention known for %s", runtime.GOARCH)
	}
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	assetName = fmt.Sprintf("sheesh-9.9.9-%s-%s.%s", runtime.GOOS, arch, ext)
	kebabName := exeName("kebab")

	if ext == "zip" {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		f, err := zw.Create(kebabName)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte("#!/bin/sh\n")); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return assetName, buf.Bytes()
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("#!/bin/sh\n")
	if err := tw.WriteHeader(&tar.Header{Name: kebabName, Mode: 0755, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return assetName, buf.Bytes()
}

func TestEnsureSheesh_NoOpWhenKebabExists(t *testing.T) {
	withHome(t)
	shimDir, err := store.ShimDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shimDir, exeName("kebab")), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	client := &fakeGHClient{latestReleaseErr: fmt.Errorf("should not be called")}
	if err := ensureSheesh(context.Background(), &config.Settings{}, client); err != nil {
		t.Errorf("expected a no-op (and no gh call) when kebab already exists, got %v", err)
	}
}

func TestEnsureSheesh_PropagatesGetLatestReleaseError(t *testing.T) {
	withHome(t)
	client := &fakeGHClient{latestReleaseErr: fmt.Errorf("network down")}

	err := ensureSheesh(context.Background(), &config.Settings{}, client)
	if err == nil {
		t.Fatal("expected the release-fetch error to propagate")
	}
}

// TestEnsureSheesh_BootstrapsFromRelease covers the real path: no kebab yet,
// so ensureSheesh downloads and extracts the platform-matching sheesh asset
// into ShimDir.
func TestEnsureSheesh_BootstrapsFromRelease(t *testing.T) {
	withHome(t)
	assetName, content := fakeSheeshAsset(t)

	client := &fakeGHClient{
		latestRelease: gh.Release{
			TagName: "v0.1.1",
			Assets:  []gh.Asset{{Name: assetName, Size: int64(len(content))}},
		},
		downloadContent: content,
	}

	if err := ensureSheesh(context.Background(), &config.Settings{}, client); err != nil {
		t.Fatal(err)
	}

	shimDir, err := store.ShimDir()
	if err != nil {
		t.Fatal(err)
	}
	kebabPath := filepath.Join(shimDir, exeName("kebab"))
	info, err := os.Stat(kebabPath)
	if err != nil {
		t.Fatalf("expected kebab to be vendored at %s: %v", kebabPath, err)
	}
	if runtime.GOOS != "windows" && info.Mode()&0111 == 0 {
		t.Error("vendored kebab should be executable")
	}
}
