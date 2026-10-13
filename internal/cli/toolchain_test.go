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
	"github.com/meop/golie/sheesh"

	"github.com/meop/ghpm/internal/store"
)

const pinnedKebab = "#!/bin/sh\necho the pinned kebab\n"

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// sheeshKey finds this platform's entry in golie's pin table.
func sheeshKey(t *testing.T) string {
	t.Helper()
	tool, ok := sheesh.Tool(runtime.GOOS, runtime.GOARCH)
	if !ok {
		t.Skipf("no sheesh release convention known for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	for key, asset := range sheesh.Assets {
		if asset.SHA256 == tool.Asset.SHA256 {
			return key
		}
	}
	t.Fatal("this platform's sheesh asset is not in the pin table")
	return ""
}

// pinSheesh makes asset what golie pins for this platform, for the length of
// the test: what a fake release serves is not what the real pin names.
func pinSheesh(t *testing.T, asset pinned.Asset) {
	t.Helper()
	key := sheeshKey(t)
	orig := sheesh.Assets[key]
	sheesh.Assets[key] = asset
	t.Cleanup(func() { sheesh.Assets[key] = orig })
}

// fakeSheeshArchive builds a release archive for the current platform
// holding one executable, kebab.
func fakeSheeshArchive(t *testing.T) []byte {
	t.Helper()
	name := exeName("kebab")
	var buf bytes.Buffer
	if runtime.GOOS == "windows" {
		zw := zip.NewWriter(&buf)
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(pinnedKebab))
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(pinnedKebab)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_, _ = tw.Write([]byte(pinnedKebab))
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// unreachableSheesh points golie's release base at a closed local port, so a
// test that hits it proves syncSheesh attempted a fetch it should have skipped.
func unreachableSheesh(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(nil)
	url := srv.URL
	srv.Close()
	orig := sheesh.ReleaseBase
	sheesh.ReleaseBase = url + "/"
	t.Cleanup(func() { sheesh.ReleaseBase = orig })
}

// serveSheeshRelease stands up a local server offering a fake release for
// the current platform, points golie at it, and pins it.
func serveSheeshRelease(t *testing.T) {
	t.Helper()
	archive := fakeSheeshArchive(t)
	pinSheesh(t, pinned.Asset{SHA256: sha(archive), Files: map[string]string{exeName("kebab"): sha([]byte(pinnedKebab))}})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	t.Cleanup(srv.Close)
	orig := sheesh.ReleaseBase
	sheesh.ReleaseBase = srv.URL + "/"
	t.Cleanup(func() { sheesh.ReleaseBase = orig })
}

func kebabPathForTest(t *testing.T) string {
	t.Helper()
	shimDir, err := store.ShimDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(shimDir, exeName("kebab"))
}

func TestSyncSheesh_NoOpWhenKebabMatchesItsPin(t *testing.T) {
	withHome(t)
	unreachableSheesh(t)
	if err := os.WriteFile(kebabPathForTest(t), []byte(pinnedKebab), 0755); err != nil {
		t.Fatal(err)
	}
	pinSheesh(t, pinned.Asset{Files: map[string]string{exeName("kebab"): sha([]byte(pinnedKebab))}})

	if err := syncSheesh(context.Background()); err != nil {
		t.Errorf("expected a no-op (and no fetch) when kebab already matches its pin, got %v", err)
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
	info, err := os.Stat(kebabPathForTest(t))
	if err != nil {
		t.Fatalf("expected kebab to be vendored: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode()&0111 == 0 {
		t.Error("vendored kebab should be executable")
	}
}

// TestSyncSheesh_ReplacesAKebabThatDoesNotMatch is the point of pinning by
// content: a kebab that merely exists is not enough, and neither is one that
// would report the right version. Whatever is not the pinned file is
// replaced, and the directory ends up holding the pinned release and nothing
// else — no file an older release shipped, nothing left of the fetch.
func TestSyncSheesh_ReplacesAKebabThatDoesNotMatch(t *testing.T) {
	withHome(t)
	kebabPath := kebabPathForTest(t)
	if err := os.WriteFile(kebabPath, []byte("#!/bin/sh\necho "+sheesh.Version+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(filepath.Dir(kebabPath), "shipped-by-an-older-release")
	if err := os.WriteFile(stale, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	serveSheeshRelease(t)

	if err := syncSheesh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(kebabPath); string(got) != pinnedKebab {
		t.Errorf("expected the kebab that does not match its pin to be replaced, got %q", got)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("a file the pinned release does not ship was left in the vendored dir")
	}
	entries, err := os.ReadDir(filepath.Dir(filepath.Dir(kebabPath)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"sheesh"}) {
		t.Errorf("the sync left %v in the vendor dir, want only sheesh", names)
	}
}

// TestSyncSheesh_RefusesAReleaseThatDoesNotMatchItsPin: the pin names exact
// contents, so a release serving anything else is not vendored and what was
// there stays.
func TestSyncSheesh_RefusesAReleaseThatDoesNotMatchItsPin(t *testing.T) {
	withHome(t)
	kebabPath := kebabPathForTest(t)
	if err := os.WriteFile(kebabPath, []byte("the old kebab"), 0755); err != nil {
		t.Fatal(err)
	}
	serveSheeshRelease(t)
	pinSheesh(t, pinned.Asset{SHA256: strings.Repeat("0", 64), Files: map[string]string{exeName("kebab"): sha([]byte(pinnedKebab))}})

	if err := syncSheesh(context.Background()); err == nil {
		t.Fatal("expected a release that does not match its pinned digest to be refused")
	}
	if got, _ := os.ReadFile(kebabPath); string(got) != "the old kebab" {
		t.Errorf("the vendored kebab was disturbed by a release that failed its check: %q", got)
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
