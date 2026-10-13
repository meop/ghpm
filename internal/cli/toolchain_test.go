package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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

// unreachableSheesh points sheeshReleaseBase at a closed local port, so a
// test that hits it proves syncSheesh attempted a fetch it should have skipped.
func unreachableSheesh(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(nil)
	url := srv.URL
	srv.Close()
	orig := sheeshReleaseBase
	sheeshReleaseBase = url + "/"
	t.Cleanup(func() { sheeshReleaseBase = orig })
}

// pinSheeshDigest makes sum the digest pinned for this platform's sheesh
// asset for the length of the test.
func pinSheeshDigest(t *testing.T, sum string) {
	t.Helper()
	_, pinned, ok := toolchain.SheeshAsset(runtime.GOOS, runtime.GOARCH)
	if !ok {
		t.Skipf("no sheesh release convention known for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	for key, value := range toolchain.SheeshDigests {
		if value == pinned {
			toolchain.SheeshDigests[key] = sum
			t.Cleanup(func() { toolchain.SheeshDigests[key] = pinned })
			return
		}
	}
}

// serveSheeshRelease stands up a local server offering the pinned release's
// asset for the current platform at the path a release download has, points
// sheeshReleaseBase at it, and pins the asset's digest.
func serveSheeshRelease(t *testing.T) {
	t.Helper()
	assetName, content := fakeSheeshAsset(t)
	sum := sha256.Sum256(content)
	pinSheeshDigest(t, hex.EncodeToString(sum[:]))

	mux := http.NewServeMux()
	mux.HandleFunc("/v"+toolchain.SheeshVersion+"/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	orig := sheeshReleaseBase
	sheeshReleaseBase = srv.URL + "/"
	t.Cleanup(func() { sheeshReleaseBase = orig })
}

func TestSyncSheesh_NoOpWhenKebabIsAtPin(t *testing.T) {
	withHome(t)
	unreachableSheesh(t)
	writeFakeKebab(t, kebabPathForTest(t), toolchain.SheeshVersion)

	if err := syncSheesh(context.Background()); err != nil {
		t.Errorf("expected a no-op (and no fetch) when kebab is already at the pin, got %v", err)
	}
}

func TestSyncSheesh_PropagatesFetchError(t *testing.T) {
	withHome(t)
	unreachableSheesh(t)

	if err := syncSheesh(context.Background()); err == nil {
		t.Fatal("expected the fetch error to propagate")
	}
}

// TestSyncSheesh_VendorsFromPinnedRelease covers the real path: no kebab yet,
// so syncSheesh downloads the pinned asset for this platform and unpacks it
// into ShimDir.
func TestSyncSheesh_VendorsFromPinnedRelease(t *testing.T) {
	withHome(t)
	serveSheeshRelease(t)

	if err := syncSheesh(context.Background()); err != nil {
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
// upgrading. The directory is replaced whole, so a file an older release
// shipped and the pinned one does not is gone afterwards, and nothing of the
// fetch is left beside it.
func TestSyncSheesh_ReplacesKebabAtWrongVersion(t *testing.T) {
	withHome(t)
	kebabPath := kebabPathForTest(t)
	writeFakeKebab(t, kebabPath, "99.99.99")
	stale := filepath.Join(filepath.Dir(kebabPath), "shipped-by-an-older-release")
	if err := os.WriteFile(stale, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	serveSheeshRelease(t)

	if err := syncSheesh(context.Background()); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(kebabPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == "#!/bin/sh\necho '99.99.99'\n" {
		t.Error("expected the off-pin kebab to be replaced")
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("a file the pinned release does not ship was left in the vendored dir")
	}
	entries, err := os.ReadDir(filepath.Dir(filepath.Dir(kebabPath)))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "sheesh" {
			t.Errorf("the sync left %s behind in the vendor dir", e.Name())
		}
	}
}

// TestSyncSheesh_RefusesAnAssetThatDoesNotMatchItsPin: the pin names exact
// contents, so a release serving anything else is not vendored and what was
// there stays.
func TestSyncSheesh_RefusesAnAssetThatDoesNotMatchItsPin(t *testing.T) {
	withHome(t)
	kebabPath := kebabPathForTest(t)
	writeFakeKebab(t, kebabPath, "99.99.99")
	serveSheeshRelease(t)
	pinSheeshDigest(t, strings.Repeat("0", 64))

	if err := syncSheesh(context.Background()); err == nil {
		t.Fatal("expected an asset that does not match its pinned digest to be refused")
	}
	got, err := os.ReadFile(kebabPath)
	if err != nil || string(got) != "#!/bin/sh\necho '99.99.99'\n" {
		t.Errorf("the vendored kebab was disturbed by an asset that failed its check: %q, %v", got, err)
	}
}

// TestSyncSheesh_DryRunWritesNothing: a dry run is about the command the user
// asked for, and vendoring the toolchain to preview it would be a real write.
func TestSyncSheesh_DryRunWritesNothing(t *testing.T) {
	withHome(t)
	unreachableSheesh(t)
	dryRun = true
	defer func() { dryRun = false }()

	if err := syncSheesh(context.Background()); err != nil {
		t.Fatalf("expected a dry run to be a silent no-op, got %v", err)
	}
	if _, err := os.Stat(kebabPathForTest(t)); err == nil {
		t.Error("expected no kebab written during a dry run")
	}
}
