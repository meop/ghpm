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
	"github.com/meop/ghpm/internal/toolchain"
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
	assetName = fmt.Sprintf("sheesh-%s-%s-%s.%s", toolchain.SheeshVersion, runtime.GOOS, arch, ext)
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

// writeFakeKebab writes an executable at path that reports ver, so
// syncSheesh's version probe has something to read.
func writeFakeKebab(t *testing.T, path, ver string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the version probe needs a runnable script; not worth a .cmd shim here")
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho '"+ver+"'\n"), 0755); err != nil {
		t.Fatal(err)
	}
}

func kebabPathForTest(t *testing.T) string {
	t.Helper()
	shimDir, err := store.ShimDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(shimDir, exeName("kebab"))
}

func TestSyncSheesh_NoOpWhenKebabIsAtPin(t *testing.T) {
	withHome(t)
	writeFakeKebab(t, kebabPathForTest(t), toolchain.SheeshVersion)

	client := &fakeGHClient{tagReleaseErr: fmt.Errorf("should not be called")}
	if err := syncSheesh(context.Background(), &config.Settings{}, client); err != nil {
		t.Errorf("expected a no-op (and no gh call) when kebab is already at the pin, got %v", err)
	}
}

func TestSyncSheesh_PropagatesReleaseLookupError(t *testing.T) {
	withHome(t)
	client := &fakeGHClient{tagReleaseErr: fmt.Errorf("network down")}

	err := syncSheesh(context.Background(), &config.Settings{}, client)
	if err == nil {
		t.Fatal("expected the release-fetch error to propagate")
	}
}

// TestSyncSheesh_VendorsFromPinnedRelease covers the real path: no kebab yet,
// so syncSheesh downloads and extracts the platform-matching sheesh asset
// into ShimDir.
func TestSyncSheesh_VendorsFromPinnedRelease(t *testing.T) {
	withHome(t)
	assetName, content := fakeSheeshAsset(t)

	client := &fakeGHClient{
		tagRelease: gh.Release{
			TagName: "v" + toolchain.SheeshVersion,
			Assets:  []gh.Asset{{Name: assetName, Size: int64(len(content))}},
		},
		downloadContent: content,
	}

	if err := syncSheesh(context.Background(), &config.Settings{}, client); err != nil {
		t.Fatal(err)
	}

	kebabPath := kebabPathForTest(t)
	info, err := os.Stat(kebabPath)
	if err != nil {
		t.Fatalf("expected kebab to be vendored at %s: %v", kebabPath, err)
	}
	if runtime.GOOS != "windows" && info.Mode()&0111 == 0 {
		t.Error("vendored kebab should be executable")
	}
}

// TestSyncSheesh_ReplacesKebabAtWrongVersion is the point of pinning: a kebab
// that merely exists isn't enough, and one at a *newer* version is replaced
// too — the sync corrects drift in either direction rather than only
// upgrading.
func TestSyncSheesh_ReplacesKebabAtWrongVersion(t *testing.T) {
	withHome(t)
	kebabPath := kebabPathForTest(t)
	writeFakeKebab(t, kebabPath, "99.99.99")

	assetName, content := fakeSheeshAsset(t)
	client := &fakeGHClient{
		tagRelease: gh.Release{
			TagName: "v" + toolchain.SheeshVersion,
			Assets:  []gh.Asset{{Name: assetName, Size: int64(len(content))}},
		},
		downloadContent: content,
	}

	if err := syncSheesh(context.Background(), &config.Settings{}, client); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(kebabPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == "#!/bin/sh\necho '99.99.99'\n" {
		t.Error("expected the off-pin kebab to be replaced")
	}
}

// TestSyncSheesh_DryRunWritesNothing: a dry run is about the command the user
// asked for, and vendoring the toolchain to preview it would be a real write.
func TestSyncSheesh_DryRunWritesNothing(t *testing.T) {
	withHome(t)
	dryRun = true
	defer func() { dryRun = false }()

	client := &fakeGHClient{tagReleaseErr: fmt.Errorf("should not be called")}
	if err := syncSheesh(context.Background(), &config.Settings{}, client); err != nil {
		t.Fatalf("expected a dry run to be a silent no-op, got %v", err)
	}
	if _, err := os.Stat(kebabPathForTest(t)); err == nil {
		t.Error("expected no kebab written during a dry run")
	}
}
