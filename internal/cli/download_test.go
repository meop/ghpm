package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/config"
)

func TestRunDownload_NoGH(t *testing.T) {
	withHome(t)
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	writeSettings(t, &config.Settings{})
	quiet = true
	defer func() { quiet = false }()

	err := runDownload(cmdWithExpiredContext(), []string{"fzf"})
	if err == nil {
		t.Fatal("expected error when gh not found")
	}
}

// TestRunDownload_FailsWhenLockHeld guards download's fix for a stale-shim-
// style gap: it writes into the same shared release cache add/sync use, but
// unlike them never took the process lock, so two writers (or a concurrent
// add/sync) could race on the same cache file. Now it does.
func TestRunDownload_FailsWhenLockHeld(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})

	unlock, err := config.AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	if err := runDownload(cmdWithContext(), []string{"fzf"}); err == nil {
		t.Fatal("expected an error while another process holds the lock")
	}
}

// TestVerifyAssetDigest_Mismatch guards download's fix for a second gap
// alongside the missing lock: it had --skip-hash-check wired into cmdOptions
// but never actually checked a digest anywhere, unlike add/sync's
// extractOverlay (helpers.go), which verifies every asset before extracting.
// download now shares the same verifyAssetDigest guard.
func TestVerifyAssetDigest_Mismatch(t *testing.T) {
	skipHashCheck = false
	defer func() { skipHashCheck = false }()
	f := filepath.Join(t.TempDir(), "pkg.tar.gz")
	if err := os.WriteFile(f, []byte("actual content"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := verifyAssetDigest("sha256:"+strings.Repeat("0", 64), f); err == nil {
		t.Fatal("expected a digest mismatch to be reported as an error")
	}
}

func TestVerifyAssetDigest_Match(t *testing.T) {
	skipHashCheck = false
	defer func() { skipHashCheck = false }()
	content := []byte("actual content")
	sum := sha256.Sum256(content)
	f := filepath.Join(t.TempDir(), "pkg.tar.gz")
	if err := os.WriteFile(f, content, 0644); err != nil {
		t.Fatal(err)
	}

	if err := verifyAssetDigest("sha256:"+hex.EncodeToString(sum[:]), f); err != nil {
		t.Fatalf("unexpected error for a matching digest: %v", err)
	}
}

func TestVerifyAssetDigest_NoDigestReported(t *testing.T) {
	skipHashCheck = false
	defer func() { skipHashCheck = false }()

	// Nonexistent path: proves the empty-digest case returns before ever
	// touching the filesystem, rather than merely failing to open it.
	if err := verifyAssetDigest("", filepath.Join(t.TempDir(), "missing.tar.gz")); err != nil {
		t.Fatalf("unexpected error when the release reports no digest: %v", err)
	}
}

func TestVerifyAssetDigest_SkipHashCheck(t *testing.T) {
	skipHashCheck = true
	defer func() { skipHashCheck = false }()

	// Same as above: a bad digest against a nonexistent path would fail
	// verification if it ran at all, so a nil error proves the skip.
	badDigest := "sha256:" + strings.Repeat("0", 64)
	if err := verifyAssetDigest(badDigest, filepath.Join(t.TempDir(), "missing.tar.gz")); err != nil {
		t.Fatalf("unexpected error with skipHashCheck set: %v", err)
	}
}

func TestRunInfo_NoGH(t *testing.T) {
	withHome(t)
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	writeSettings(t, &config.Settings{})
	quiet = true
	defer func() { quiet = false }()

	err := runInfo(cmdWithExpiredContext(), []string{"fzf"})
	if err == nil {
		t.Fatal("expected error when gh not found")
	}
}

func TestRunOutdated_NoGH(t *testing.T) {
	withHome(t)
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	writeSettings(t, &config.Settings{})
	writeManifest(t, &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0"}},
	})

	err := runOutdated(cmdWithExpiredContext(), []string{})
	if err == nil {
		t.Fatal("expected error when gh not found")
	}
}
