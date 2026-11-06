package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	golievendored "github.com/meop/golie/vendored"
	"github.com/meop/golie/wedge"

	"github.com/meop/ghpm/internal/faketool"
	"github.com/meop/ghpm/internal/store"
)

const fakeMalletBody = "#!/bin/sh\necho the fake mallet\n"

// fakeWedgeArchive builds a release archive for the current platform
// holding wedge's files, mallet's being body.
func fakeWedgeArchive(t *testing.T, r golievendored.Release, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if runtime.GOOS == "windows" {
		zw := zip.NewWriter(&buf)
		for _, name := range r.Files {
			f, err := zw.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = f.Write([]byte(body))
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range r.Files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pointWedgeAt points golie at a release page, unsigned: the signature check
// is golie's, and tested there
func pointWedgeAt(t *testing.T, url string) {
	t.Helper()
	origURL, origKey := wedge.ReleasesURL, wedge.Key
	wedge.ReleasesURL, wedge.Key = url, nil
	t.Cleanup(func() { wedge.ReleasesURL, wedge.Key = origURL, origKey })
}

// serveWedgeRelease stands up wedge's release page with version as its
// latest, mallet in it being body, and points golie at it. It returns the
// count of archive downloads.
func serveWedgeRelease(t *testing.T, version, body string) *int {
	t.Helper()
	r, err := wedge.Here()
	if err != nil {
		t.Skip(err)
	}
	archiveName, _ := r.Archive(version)
	files := map[string][]byte{archiveName: fakeWedgeArchive(t, r, body)}
	files["SHA256SUMS"] = faketool.Sums(files)
	url, fetches := faketool.Release(t, version, files, "SHA256SUMS")
	pointWedgeAt(t, url)
	return fetches
}

func malletPathForTest(t *testing.T) string {
	t.Helper()
	shimDir, err := store.ShimDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(shimDir, exeName("mallet"))
}

func TestSyncWedge_PropagatesFetchError(t *testing.T) {
	withHome(t)
	pointWedgeAt(t, faketool.Unreachable(t))

	if err := syncWedge(context.Background()); err == nil {
		t.Fatal("expected the fetch error to propagate")
	}
}

// TestSyncWedge_VendorsTheLatestOnceAndThenStaysOffline covers the real
// path: no mallet yet, so syncWedge downloads wedge's latest for this
// platform into ShimDir; after that, it uses what is there and asks nothing.
func TestSyncWedge_VendorsTheLatestOnceAndThenStaysOffline(t *testing.T) {
	withHome(t)
	fetches := serveWedgeRelease(t, "1.0.0", fakeMalletBody)

	if err := syncWedge(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(malletPathForTest(t))
	if err != nil {
		t.Fatalf("expected mallet to be vendored: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode()&0111 == 0 {
		t.Error("vendored mallet should be executable")
	}

	serveWedgeRelease(t, "2.0.0", "a newer mallet")
	if err := syncWedge(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(malletPathForTest(t)); string(got) != fakeMalletBody || *fetches != 1 {
		t.Errorf("a second sync moved mallet to %q: only refresh may", got)
	}
}

// TestRefreshWedge_MovesToTheLatestAndLeavesNothingElse: a refresh takes
// wedge's latest, and the directory ends up holding that release and nothing
// else — no file an older release shipped, nothing left of the fetch.
func TestRefreshWedge_MovesToTheLatestAndLeavesNothingElse(t *testing.T) {
	withHome(t)
	serveWedgeRelease(t, "1.0.0", fakeMalletBody)
	if err := syncWedge(context.Background()); err != nil {
		t.Fatal(err)
	}
	malletPath := malletPathForTest(t)
	stale := filepath.Join(filepath.Dir(malletPath), "shipped-by-an-older-release")
	if err := os.WriteFile(stale, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	serveWedgeRelease(t, "2.0.0", "a newer mallet")

	from, to, err := refreshWedge(context.Background())
	if err != nil || from != "1.0.0" || to != "2.0.0" {
		t.Fatalf("refreshWedge = %q → %q, %v", from, to, err)
	}
	if got, _ := os.ReadFile(malletPath); string(got) != "a newer mallet" {
		t.Errorf("mallet is %q", got)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("a file the release does not ship was left in the vendored dir")
	}
	entries, err := os.ReadDir(filepath.Dir(filepath.Dir(malletPath)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"wedge"}) {
		t.Errorf("the refresh left %v in the vendor dir, want only wedge", names)
	}
}

// TestSyncWedge_DryRunWritesNothing: a dry run is about the command the user
// asked for, and vendoring the toolchain to preview it would be a real write.
func TestSyncWedge_DryRunWritesNothing(t *testing.T) {
	withHome(t)
	pointWedgeAt(t, faketool.Unreachable(t))
	dryRun = true
	defer func() { dryRun = false }()

	if err := syncWedge(context.Background()); err != nil {
		t.Fatalf("expected a dry run to be a silent no-op, got %v", err)
	}
	if _, err := os.Stat(malletPathForTest(t)); err == nil {
		t.Error("expected no mallet written during a dry run")
	}
}
