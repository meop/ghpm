package ghbin

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
	"strings"
	"testing"
	"time"

	golievendored "github.com/meop/golie/vendored"

	"github.com/meop/ghpm/internal/faketool"
	"github.com/meop/ghpm/internal/toolchain"
)

const fakeGh = "fake gh binary"

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

// writeVendoredGh is writeGh with the version file a vendored release leaves,
// so it reads as gh version installed.
func writeVendoredGh(t *testing.T, content, version string) string {
	t.Helper()
	path := writeGh(t, content)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), golievendored.VersionFile), []byte(version+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return path
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

func pointGhAt(t *testing.T, url string) {
	t.Helper()
	orig := toolchain.GhReleasesURL
	toolchain.GhReleasesURL = url
	t.Cleanup(func() { toolchain.GhReleasesURL = orig })
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

// serveGhRelease stands up gh's release page with version as its latest,
// holding this platform's archive and a checksums.txt that lists it, or lists
// sum for it when sum is not empty, and points toolchain at it.
func serveGhRelease(t *testing.T, version, sum string) {
	t.Helper()
	r, ok := toolchain.Gh(runtime.GOOS, runtime.GOARCH)
	if !ok {
		t.Skipf("gh ships nothing for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	archiveName, _ := r.Archive(version)
	files := map[string][]byte{archiveName: fakeGhArchive(t)}
	sums := faketool.Sums(files)
	if sum != "" {
		sums = []byte(sum + "  " + archiveName + "\n")
	}
	files[r.Sums(version)] = sums
	url, _ := faketool.Release(t, version, files, r.Sums(version))
	pointGhAt(t, url)
}

func TestEnsure_NoOpWhenAGhIsVendored(t *testing.T) {
	withHome(t)
	pointGhAt(t, faketool.Unreachable(t))
	writeVendoredGh(t, "the old gh", "1.0.0")

	if err := Ensure(context.Background(), nil); err != nil {
		t.Errorf("expected no error (and no fetch) when a gh is vendored, got %v", err)
	}
}

// TestEnsure_BootstrapsEvenWhenPathHasGh is the core of this design: ghpm's
// own gh use never depends on PATH, so Ensure vendors its own copy even when
// a perfectly usable gh already sits on PATH.
func TestEnsure_BootstrapsEvenWhenPathHasGh(t *testing.T) {
	withHome(t)
	fakePathGH(t)
	serveGhRelease(t, "2.0.0", "")

	if err := Ensure(context.Background(), nil); err != nil {
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

	if err := Ensure(ctx, nil); err == nil {
		t.Fatal("expected an error when nothing is vendored and the context is already expired")
	}
}

// TestEnsure_BootstrapsWhenNothingFound covers the real path: nothing
// vendored, nothing on PATH, so Ensure fetches gh's latest release and
// vendors the binary itself, saying which archive it fetches.
func TestEnsure_BootstrapsWhenNothingFound(t *testing.T) {
	withHome(t)
	t.Setenv("PATH", t.TempDir())
	serveGhRelease(t, "2.0.0", "")

	var fetched []string
	if err := Ensure(context.Background(), func(a string) { fetched = append(fetched, a) }); err != nil {
		t.Fatal(err)
	}
	if len(fetched) != 1 || !strings.HasPrefix(fetched[0], "gh_2.0.0_") {
		t.Errorf("fetching was told %v", fetched)
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

// TestRefresh_MovesToTheLatest: a vendored gh stays as it is until a
// refresh, which takes gh's latest release
func TestRefresh_MovesToTheLatest(t *testing.T) {
	withHome(t)
	vendored := writeVendoredGh(t, "the old gh", "1.0.0")
	serveGhRelease(t, "2.0.0", "")

	if err := Ensure(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(vendored); string(got) != "the old gh" {
		t.Fatal("Ensure moved a vendored gh: only refresh may")
	}
	from, to, err := Refresh(context.Background(), nil)
	if err != nil || from != "1.0.0" || to != "2.0.0" {
		t.Fatalf("Refresh = %q → %q, %v", from, to, err)
	}
	if got, _ := os.ReadFile(vendored); string(got) != fakeGh {
		t.Errorf("the vendored gh is %q", got)
	}
	if from, to, err := Refresh(context.Background(), nil); err != nil || from != "2.0.0" || to != "2.0.0" {
		t.Errorf("a refresh when current = %q → %q, %v", from, to, err)
	}
}

// TestRefresh_RefusesAReleaseThatDoesNotMatchItsChecksums: an archive its
// release's checksums.txt does not list is not vendored, and what was
// vendored before stays.
func TestRefresh_RefusesAReleaseThatDoesNotMatchItsChecksums(t *testing.T) {
	withHome(t)
	vendored := writeVendoredGh(t, "the old gh", "1.0.0")
	serveGhRelease(t, "2.0.0", strings.Repeat("0", 64))

	if _, _, err := Refresh(context.Background(), nil); err == nil {
		t.Fatal("expected a release that does not match its checksums to be refused")
	}
	if got, _ := os.ReadFile(vendored); string(got) != "the old gh" {
		t.Error("the vendored gh was replaced by a release that failed its check")
	}
}

// TestRefresh_ReplacesCleanly: a refresh leaves the vendor dir holding gh,
// its version file and gh's own config — ghpm's token, never touched — and
// nothing else: no file an older gh left, no archive, no work dir.
func TestRefresh_ReplacesCleanly(t *testing.T) {
	withHome(t)
	vendored := writeVendoredGh(t, "the old gh", "1.0.0")
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
	serveGhRelease(t, "2.0.0", "")

	if _, _, err := Refresh(context.Background(), nil); err != nil {
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
	want := []string{golievendored.VersionFile, configDirName, vendorName()}
	slices.Sort(want)
	if got := list(filepath.Dir(vendored)); !slices.Equal(got, want) {
		t.Errorf("gh's vendor dir holds %v, want %v", got, want)
	}
	if got := list(filepath.Dir(filepath.Dir(vendored))); !slices.Equal(got, []string{"gh"}) {
		t.Errorf("the refresh left %v in the vendor dir, want only gh", got)
	}
	if got, _ := os.ReadFile(filepath.Join(cfgDir, "hosts.yml")); string(got) != "token" {
		t.Error("gh's config was disturbed by replacing gh")
	}
}
