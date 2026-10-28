package cli

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
	"slices"
	"strings"
	"testing"

	"github.com/meop/golie/pinned"
	"github.com/meop/golie/wedge"

	"github.com/meop/ghpm/internal/store"
)

const pinnedMallet = "#!/bin/sh\necho the pinned mallet\n"

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// wedgeKey finds this platform's entry in golie's pin table.
func wedgeKey(t *testing.T) string {
	t.Helper()
	tool, ok := wedge.Tool(runtime.GOOS, runtime.GOARCH)
	if !ok {
		t.Skipf("no wedge release convention known for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	for key, asset := range wedge.Assets {
		if asset.SHA256 == tool.Asset.SHA256 {
			return key
		}
	}
	t.Fatal("this platform's wedge asset is not in the pin table")
	return ""
}

// pinWedge makes asset what golie pins for this platform, for the length of
// the test: what a fake release serves is not what the real pin names.
func pinWedge(t *testing.T, asset pinned.Asset) {
	t.Helper()
	key := wedgeKey(t)
	orig := wedge.Assets[key]
	wedge.Assets[key] = asset
	t.Cleanup(func() { wedge.Assets[key] = orig })
}

// fakeWedgeArchive builds a release archive for the current platform
// holding one executable, mallet.
func fakeWedgeArchive(t *testing.T) []byte {
	t.Helper()
	name := exeName("mallet")
	var buf bytes.Buffer
	if runtime.GOOS == "windows" {
		zw := zip.NewWriter(&buf)
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(pinnedMallet))
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(pinnedMallet)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_, _ = tw.Write([]byte(pinnedMallet))
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// unreachableWedge points golie's release base at a closed local port, so a
// test that hits it proves syncWedge attempted a fetch it should have skipped.
func unreachableWedge(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(nil)
	url := srv.URL
	srv.Close()
	orig := wedge.ReleaseBase
	wedge.ReleaseBase = url + "/"
	t.Cleanup(func() { wedge.ReleaseBase = orig })
}

// serveWedgeRelease stands up a local server offering a fake release for
// the current platform, points golie at it, and pins it.
func serveWedgeRelease(t *testing.T) {
	t.Helper()
	archive := fakeWedgeArchive(t)
	pinWedge(t, pinned.Asset{SHA256: sha(archive), Files: map[string]string{exeName("mallet"): sha([]byte(pinnedMallet))}})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	t.Cleanup(srv.Close)
	orig := wedge.ReleaseBase
	wedge.ReleaseBase = srv.URL + "/"
	t.Cleanup(func() { wedge.ReleaseBase = orig })
}

func malletPathForTest(t *testing.T) string {
	t.Helper()
	shimDir, err := store.ShimDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(shimDir, exeName("mallet"))
}

func TestSyncWedge_NoOpWhenMalletMatchesItsPin(t *testing.T) {
	withHome(t)
	unreachableWedge(t)
	if err := os.WriteFile(malletPathForTest(t), []byte(pinnedMallet), 0755); err != nil {
		t.Fatal(err)
	}
	pinWedge(t, pinned.Asset{Files: map[string]string{exeName("mallet"): sha([]byte(pinnedMallet))}})

	if err := syncWedge(context.Background()); err != nil {
		t.Errorf("expected a no-op (and no fetch) when mallet already matches its pin, got %v", err)
	}
}

func TestSyncWedge_PropagatesFetchError(t *testing.T) {
	withHome(t)
	unreachableWedge(t)

	if err := syncWedge(context.Background()); err == nil {
		t.Fatal("expected the fetch error to propagate")
	}
}

// TestSyncWedge_VendorsFromPinnedRelease covers the real path: no mallet yet,
// so syncWedge downloads the pinned asset for this platform and unpacks it
// into ShimDir.
func TestSyncWedge_VendorsFromPinnedRelease(t *testing.T) {
	withHome(t)
	serveWedgeRelease(t)

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
}

// TestSyncWedge_ReplacesAMalletThatDoesNotMatch is the point of pinning by
// content: a mallet that merely exists is not enough, and neither is one that
// would report the right version. Whatever is not the pinned file is
// replaced, and the directory ends up holding the pinned release and nothing
// else — no file an older release shipped, nothing left of the fetch.
func TestSyncWedge_ReplacesAMalletThatDoesNotMatch(t *testing.T) {
	withHome(t)
	malletPath := malletPathForTest(t)
	if err := os.WriteFile(malletPath, []byte("#!/bin/sh\necho "+wedge.Version+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(filepath.Dir(malletPath), "shipped-by-an-older-release")
	if err := os.WriteFile(stale, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	serveWedgeRelease(t)

	if err := syncWedge(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(malletPath); string(got) != pinnedMallet {
		t.Errorf("expected the mallet that does not match its pin to be replaced, got %q", got)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("a file the pinned release does not ship was left in the vendored dir")
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
		t.Errorf("the sync left %v in the vendor dir, want only wedge", names)
	}
}

// TestSyncWedge_RefusesAReleaseThatDoesNotMatchItsPin: the pin names exact
// contents, so a release serving anything else is not vendored and what was
// there stays.
func TestSyncWedge_RefusesAReleaseThatDoesNotMatchItsPin(t *testing.T) {
	withHome(t)
	malletPath := malletPathForTest(t)
	if err := os.WriteFile(malletPath, []byte("the old mallet"), 0755); err != nil {
		t.Fatal(err)
	}
	serveWedgeRelease(t)
	pinWedge(t, pinned.Asset{SHA256: strings.Repeat("0", 64), Files: map[string]string{exeName("mallet"): sha([]byte(pinnedMallet))}})

	if err := syncWedge(context.Background()); err == nil {
		t.Fatal("expected a release that does not match its pinned digest to be refused")
	}
	if got, _ := os.ReadFile(malletPath); string(got) != "the old mallet" {
		t.Errorf("the vendored mallet was disturbed by a release that failed its check: %q", got)
	}
}

// TestSyncWedge_DryRunWritesNothing: a dry run is about the command the user
// asked for, and vendoring the toolchain to preview it would be a real write.
func TestSyncWedge_DryRunWritesNothing(t *testing.T) {
	withHome(t)
	unreachableWedge(t)
	dryRun = true
	defer func() { dryRun = false }()

	if err := syncWedge(context.Background()); err != nil {
		t.Fatalf("expected a dry run to be a silent no-op, got %v", err)
	}
	if _, err := os.Stat(malletPathForTest(t)); err == nil {
		t.Error("expected no mallet written during a dry run")
	}
}
