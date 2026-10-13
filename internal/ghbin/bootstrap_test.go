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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/meop/golie/pinned"

	"github.com/meop/ghpm/internal/toolchain"
)

const fakeGh = "fake gh binary"

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// pinGh makes asset what toolchain pins for this platform, for the length of
// the test: what a fake release serves is not what the real pin names.
func pinGh(t *testing.T, asset pinned.Asset) {
	t.Helper()
	key := toolchain.GhKey(runtime.GOOS, runtime.GOARCH)
	orig, ok := toolchain.GhAssets[key]
	if !ok {
		t.Skipf("no gh release convention known for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	toolchain.GhAssets[key] = asset
	t.Cleanup(func() { toolchain.GhAssets[key] = orig })
}

func writeGh(t *testing.T, content string) string {
	t.Helper()
	vendored, err := VendorPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(vendored), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vendored, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
	return vendored
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

// unreachableRelease points the gh release base at a closed local port, so a
// test that hits it proves Ensure attempted a fetch it should have skipped.
func unreachableRelease(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(nil)
	url := srv.URL
	srv.Close()
	orig := toolchain.GhReleaseBase
	toolchain.GhReleaseBase = url + "/"
	t.Cleanup(func() { toolchain.GhReleaseBase = orig })
}

// fakeGhArchive builds a release archive in the format this platform's gh
// ships in, nesting the binary the way the real one does.
func fakeGhArchive(t *testing.T) []byte {
	t.Helper()
	name := "gh_x/bin/" + vendorName()
	var buf bytes.Buffer
	if runtime.GOOS == "linux" {
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(fakeGh)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(fakeGh))
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	zw := zip.NewWriter(&buf)
	f, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte(fakeGh))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// serveGhRelease stands up a local server offering a fake release for the
// current platform, points toolchain at it, and pins it.
func serveGhRelease(t *testing.T) {
	t.Helper()
	archive := fakeGhArchive(t)
	pinGh(t, pinned.Asset{SHA256: sha(archive), Files: map[string]string{vendorName(): sha([]byte(fakeGh))}})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	t.Cleanup(srv.Close)
	orig := toolchain.GhReleaseBase
	toolchain.GhReleaseBase = srv.URL + "/"
	t.Cleanup(func() { toolchain.GhReleaseBase = orig })
}

func TestEnsure_NoOpWhenVendoredMatchesItsPin(t *testing.T) {
	withHome(t)
	unreachableRelease(t)
	writeGh(t, fakeGh)
	pinGh(t, pinned.Asset{Files: map[string]string{vendorName(): sha([]byte(fakeGh))}})

	if err := Ensure(context.Background()); err != nil {
		t.Errorf("expected no error (and no fetch) when gh already matches its pin, got %v", err)
	}
}

// TestEnsure_ReplacesAGhThatDoesNotMatch is the point of pinning by content:
// a gh that merely exists is not enough, and neither is one that would
// report the pinned version — only the pinned binary passes.
func TestEnsure_ReplacesAGhThatDoesNotMatch(t *testing.T) {
	withHome(t)
	vendored := writeGh(t, "#!/bin/sh\necho 'gh version "+toolchain.GhVersion+" (2026-01-01)'\n")
	serveGhRelease(t)

	if err := Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(vendored); string(got) != fakeGh {
		t.Errorf("expected the gh that does not match its pin to be replaced, got %q", got)
	}
}

// TestEnsure_BootstrapsEvenWhenPathHasGh is the core of this design: ghpm's
// own gh use never depends on PATH, so Ensure vendors its own copy even when
// a perfectly usable gh already sits on PATH.
func TestEnsure_BootstrapsEvenWhenPathHasGh(t *testing.T) {
	withHome(t)
	fakePathGH(t)
	serveGhRelease(t)

	if err := Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	vendored, err := VendorPath()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(vendored); err != nil || string(got) != fakeGh {
		t.Fatalf("expected a vendored gh despite PATH already having one: %q, %v", got, err)
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
		t.Fatal("expected an error when nothing is vendored and the context is already expired")
	}
}

// TestEnsure_BootstrapsWhenNothingFound covers the real path: nothing
// vendored, nothing on PATH, so Ensure fetches the pinned gh release and
// vendors the binary itself.
func TestEnsure_BootstrapsWhenNothingFound(t *testing.T) {
	withHome(t)
	t.Setenv("PATH", t.TempDir())
	serveGhRelease(t)

	if err := Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	vendored, err := VendorPath()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(vendored); err != nil || string(got) != fakeGh {
		t.Fatalf("vendored gh not written: %q, %v", got, err)
	}
	info, err := os.Stat(vendored)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode()&0111 == 0 {
		t.Error("vendored gh should be executable")
	}
}

// TestEnsure_RefusesAReleaseThatDoesNotMatchItsPin: the pin names exact
// contents, so a release that serves anything else is not vendored, and what
// was vendored before stays.
func TestEnsure_RefusesAReleaseThatDoesNotMatchItsPin(t *testing.T) {
	withHome(t)
	vendored := writeGh(t, "the old gh")
	serveGhRelease(t)
	pinGh(t, pinned.Asset{SHA256: strings.Repeat("0", 64), Files: map[string]string{vendorName(): sha([]byte(fakeGh))}})

	if err := Ensure(context.Background()); err == nil {
		t.Fatal("expected a release that does not match its pinned digest to be refused")
	}
	if got, _ := os.ReadFile(vendored); string(got) != "the old gh" {
		t.Error("the vendored gh was replaced by a release that failed its check")
	}
}

// TestEnsure_ReplacesCleanly: a replace leaves the vendor dir holding the
// pinned gh and gh's own config — ghpm's token, never touched — and nothing
// else: no file an older gh left, no archive, no work dir.
func TestEnsure_ReplacesCleanly(t *testing.T) {
	withHome(t)
	vendored := writeGh(t, "the old gh")
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
	if err := os.WriteFile(filepath.Join(filepath.Dir(vendored), "left-by-an-older-gh"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	serveGhRelease(t)

	if err := Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	list := func(dir string) []string {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		slices.Sort(names)
		return names
	}
	if got := list(filepath.Dir(vendored)); !slices.Equal(got, []string{".pin", vendorName(), configDirName}) && !slices.Equal(got, []string{".pin", configDirName, vendorName()}) {
		t.Errorf("gh's vendor dir holds %v, want only gh, its config dir and the pin stamp", got)
	}
	if got := list(filepath.Dir(filepath.Dir(vendored))); !slices.Equal(got, []string{"gh"}) {
		t.Errorf("the sync left %v in the vendor dir, want only gh", got)
	}
	if got, _ := os.ReadFile(filepath.Join(cfgDir, "hosts.yml")); string(got) != "token" {
		t.Error("gh's config was disturbed by replacing gh")
	}
}
