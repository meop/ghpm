package ghbin

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
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/meop/ghpm/internal/toolchain"
)

// writeFakeGh writes an executable at path that reports ver the way real gh
// does, so Ensure's version probe has something to read.
func writeFakeGh(t *testing.T, path, ver string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the version probe needs a runnable script; not worth a .cmd shim here")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'gh version "+ver+" (2026-01-01)'\n"), 0755); err != nil {
		t.Fatal(err)
	}
}

func fakePathGH(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "gh")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// unreachableAPI points ghReleaseBase at a closed local port, so a test that
// hits it proves Ensure attempted a network call it should have skipped.
func unreachableAPI(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(nil)
	url := srv.URL
	srv.Close()
	orig := ghReleaseBase
	ghReleaseBase = url + "/"
	t.Cleanup(func() { ghReleaseBase = orig })
}

// pinDigest makes sum the digest pinned for this platform's gh asset for the
// length of the test: what a fake release serves is not what the real pin
// names.
func pinDigest(t *testing.T, sum string) {
	t.Helper()
	_, pinned, ok := toolchain.GhAsset(runtime.GOOS, runtime.GOARCH)
	if !ok {
		t.Skipf("no gh release convention known for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	for key, value := range toolchain.GhDigests {
		if value == pinned {
			toolchain.GhDigests[key] = sum
			t.Cleanup(func() { toolchain.GhDigests[key] = pinned })
			return
		}
	}
}

// serveGhRelease stands up a local server offering the pinned release's asset
// for the current platform (built by fakeGhAsset) at the path a release
// download has, points ghReleaseBase at it, and pins the asset's digest.
func serveGhRelease(t *testing.T, binName, content string) {
	t.Helper()
	assetName, archive := fakeGhAsset(t, binName, content)
	sum := sha256.Sum256(archive)
	pinDigest(t, hex.EncodeToString(sum[:]))

	mux := http.NewServeMux()
	mux.HandleFunc("/v"+toolchain.GhVersion+"/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	orig := ghReleaseBase
	ghReleaseBase = srv.URL + "/"
	t.Cleanup(func() { ghReleaseBase = orig })
}

func TestEnsure_NoOpWhenVendoredIsAtPin(t *testing.T) {
	withHome(t)
	unreachableAPI(t)
	vendored, err := VendorPath()
	if err != nil {
		t.Fatal(err)
	}
	writeFakeGh(t, vendored, toolchain.GhVersion)

	if err := Ensure(context.Background()); err != nil {
		t.Errorf("expected no error (and no network call) when already at the pin, got %v", err)
	}
}

// TestEnsure_ReplacesVendoredAtWrongVersion is the point of pinning: what is
// already vendored is only kept when it is the pinned version, and a copy at
// any other version is replaced — the newer version here proves the sync is
// not a one-way "upgrade if older" check.
func TestEnsure_ReplacesVendoredAtWrongVersion(t *testing.T) {
	withHome(t)
	vendored, err := VendorPath()
	if err != nil {
		t.Fatal(err)
	}
	writeFakeGh(t, vendored, "99.99.99")
	serveGhRelease(t, vendorName(), "fake gh binary")

	if err := Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(vendored)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "fake gh binary" {
		t.Errorf("expected the off-pin gh to be replaced, got %q", got)
	}
}

// TestEnsure_BootstrapsEvenWhenPathHasGh is the core of this design: ghpm's
// own gh use never depends on PATH, so Ensure vendors its own copy even when
// a perfectly usable gh already sits on PATH.
func TestEnsure_BootstrapsEvenWhenPathHasGh(t *testing.T) {
	withHome(t)
	fakePathGH(t)
	serveGhRelease(t, vendorName(), "fake gh binary")

	if err := Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}

	vendored, err := VendorPath()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(vendored)
	if err != nil {
		t.Fatalf("expected a vendored gh despite PATH already having one: %v", err)
	}
	if string(got) != "fake gh binary" {
		t.Errorf("vendored gh has wrong content: %q", got)
	}
}

// TestAuthIfNeeded_SkippedWhenNotInteractive covers the gate itself: go
// test's stdin isn't a terminal, so this must return before ever trying to
// exec ghPath — which doesn't exist, so any attempt would error.
func TestAuthIfNeeded_SkippedWhenNotInteractive(t *testing.T) {
	ghPath := filepath.Join(t.TempDir(), "not-a-real-gh")
	if err := authIfNeeded(context.Background(), ghPath); err != nil {
		t.Errorf("expected a no-op when non-interactive, got %v", err)
	}
}

func TestEnsure_ExpiredContextFailsFast(t *testing.T) {
	withHome(t)
	t.Setenv("PATH", t.TempDir())

	ctx, cancel := context.WithDeadline(context.Background(), time.Now())
	defer cancel()

	if err := Ensure(ctx); err == nil {
		t.Fatal("expected an error when nothing is findable and the context is already expired")
	}
}

// TestEnsure_BootstrapsWhenNothingFound covers the real path: nothing
// vendored, nothing on PATH, so Ensure fetches the latest gh release and
// vendors the binary itself.
func TestEnsure_BootstrapsWhenNothingFound(t *testing.T) {
	withHome(t)
	t.Setenv("PATH", t.TempDir())
	serveGhRelease(t, vendorName(), "fake gh binary")

	if err := Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}

	vendored, err := VendorPath()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(vendored)
	if err != nil {
		t.Fatalf("vendored gh not written: %v", err)
	}
	if string(got) != "fake gh binary" {
		t.Errorf("vendored gh has wrong content: %q", got)
	}
	info, err := os.Stat(vendored)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0111 == 0 {
		t.Error("vendored gh should be executable")
	}
}

// TestEnsure_RefusesAnAssetThatDoesNotMatchItsPin: the pin names exact
// contents, so a release that serves anything else is not vendored, and what
// was vendored before stays.
func TestEnsure_RefusesAnAssetThatDoesNotMatchItsPin(t *testing.T) {
	withHome(t)
	vendored, err := VendorPath()
	if err != nil {
		t.Fatal(err)
	}
	writeFakeGh(t, vendored, "99.99.99")
	before, err := os.ReadFile(vendored)
	if err != nil {
		t.Fatal(err)
	}
	serveGhRelease(t, vendorName(), "fake gh binary")
	pinDigest(t, strings.Repeat("0", 64))

	if err := Ensure(context.Background()); err == nil {
		t.Fatal("expected an asset that does not match its pinned digest to be refused")
	}
	if got, _ := os.ReadFile(vendored); string(got) != string(before) {
		t.Error("the vendored gh was replaced by an asset that failed its check")
	}
}

// TestEnsure_ReplacesCleanly: a replace leaves the vendor dir holding the new
// gh and gh's own config, and nothing else — no archive, no work dir.
func TestEnsure_ReplacesCleanly(t *testing.T) {
	withHome(t)
	vendored, err := VendorPath()
	if err != nil {
		t.Fatal(err)
	}
	writeFakeGh(t, vendored, "99.99.99")
	cfgDir, err := ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "hosts.yml"), []byte("token"), 0600); err != nil {
		t.Fatal(err)
	}
	serveGhRelease(t, vendorName(), "fake gh binary")

	if err := Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(vendored))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 2 {
		t.Errorf("vendor dir holds %v, want only gh and its config dir", names)
	}
	if got, _ := os.ReadFile(filepath.Join(cfgDir, "hosts.yml")); string(got) != "token" {
		t.Error("gh's config was disturbed by replacing gh")
	}
}

// fakeGhAsset builds a release asset in the archive format and naming
// convention the pinned gh release uses for this platform, containing a single file
// named binName.
func fakeGhAsset(t *testing.T, binName, content string) (assetName string, archive []byte) {
	t.Helper()
	assetName, _, ok := toolchain.GhAsset(runtime.GOOS, runtime.GOARCH)
	if !ok {
		t.Skipf("no gh release convention known for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	ext := ".zip"
	if strings.HasSuffix(assetName, ".tar.gz") {
		ext = ".tar.gz"
	}

	switch ext {
	case ".tar.gz":
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{Name: binName, Mode: 0755, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		return assetName, buf.Bytes()
	case ".zip":
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		f, err := zw.Create(binName)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return assetName, buf.Bytes()
	default:
		t.Fatalf("unhandled archive extension %q", ext)
		return "", nil
	}
}
