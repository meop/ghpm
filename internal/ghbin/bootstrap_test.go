package ghbin

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func fakePathGH(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "gh")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// unreachableAPI points ghReleaseAPI at a closed local port, so a test that
// hits it proves Ensure attempted a network call it should have skipped.
func unreachableAPI(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(nil)
	url := srv.URL
	srv.Close()
	orig := ghReleaseAPI
	ghReleaseAPI = url
	t.Cleanup(func() { ghReleaseAPI = orig })
}

func TestEnsure_NoOpWhenVendoredAlreadyExists(t *testing.T) {
	withHome(t)
	unreachableAPI(t)
	vendored, err := VendorPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(vendored), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vendored, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := Ensure(context.Background()); err != nil {
		t.Errorf("expected no error (and no network call) when already vendored, got %v", err)
	}
}

func TestEnsure_NoOpWhenPathHasGh(t *testing.T) {
	withHome(t)
	unreachableAPI(t)
	fakePathGH(t)

	if err := Ensure(context.Background()); err != nil {
		t.Errorf("expected no error (and no network call) when PATH already has gh, got %v", err)
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

	binName := vendorName()
	assetName, archive := fakeGhAsset(t, binName, "fake gh binary")

	mux := http.NewServeMux()
	mux.HandleFunc("/download/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/repos/cli/cli/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"assets": []map[string]string{
				{"name": assetName, "browser_download_url": srv.URL + "/download/" + assetName},
			},
		})
	})
	orig := ghReleaseAPI
	ghReleaseAPI = srv.URL + "/repos/cli/cli/releases/latest"
	t.Cleanup(func() { ghReleaseAPI = orig })

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

// fakeGhAsset builds a release asset in the archive format and naming
// convention this platform's ghAssetPattern expects, containing a single file
// named binName.
func fakeGhAsset(t *testing.T, binName, content string) (assetName string, archive []byte) {
	t.Helper()
	pattern, ext := ghAssetPattern()
	if pattern == "" {
		t.Skipf("no gh release convention known for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	assetName = "gh_9.9.9_" + pattern + ext

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
