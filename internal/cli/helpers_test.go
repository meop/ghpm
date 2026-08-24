package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/store"
	"github.com/meop/ghpm/internal/ui"
)

func writeSettings(t *testing.T, s *config.Settings) {
	t.Helper()
	dir, err := store.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := toml.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func fakeGHBin(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "gh")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	path := dir
	if filepath.Separator != '\\' {
		path += string(os.PathListSeparator) + os.Getenv("PATH")
	}
	t.Setenv("PATH", path)
}

func TestReservedShimNames_ExcludesOwner(t *testing.T) {
	manifest := &config.Manifest{
		Extracts: map[string]config.PackageEntry{
			"fzf":    {Bin: map[string]string{"fzf": "fzf"}},
			"uv":     {Bin: map[string]string{"uv": "uv", "uvx": "uvx"}},
			"uv@0.7": {Bin: map[string]string{"uv@0.7": "uv"}},
		},
	}

	reserved := reservedShimNames(manifest, "uv")

	if owner, ok := reserved["fzf"]; !ok || owner != "fzf" {
		t.Errorf("expected fzf reserved by fzf, got %q ok=%v", owner, ok)
	}
	// Both the unversioned and versioned uv entries share the owner "uv" and must be excluded.
	if _, ok := reserved["uv"]; ok {
		t.Error("uv shim should be excluded as it belongs to the owner")
	}
	if _, ok := reserved["uvx"]; ok {
		t.Error("uvx shim should be excluded as it belongs to the owner")
	}
	if _, ok := reserved["uv@0.7"]; ok {
		t.Error("versioned uv shim should be excluded — same owner")
	}
}

func TestReservedFontNames_ExcludesOwner(t *testing.T) {
	manifest := &config.Manifest{
		Extracts: map[string]config.PackageEntry{
			"nerd-fonts":  {Font: map[string]string{"hack": "Hack-Regular.ttf"}},
			"other-fonts": {Font: map[string]string{"mono": "Mono.ttf"}},
		},
	}

	reserved := reservedFontNames(manifest, "nerd-fonts")

	if _, ok := reserved["hack"]; ok {
		t.Error("hack font should be excluded as it belongs to the owner")
	}
	if owner, ok := reserved["mono"]; !ok || owner != "other-fonts" {
		t.Errorf("expected mono reserved by other-fonts, got %q ok=%v", owner, ok)
	}
}

func TestSameKeySetFold_ExactMatch(t *testing.T) {
	recase, ok := sameKeySetFold(
		[]string{"Hack-Regular.ttf", "Hack-Bold.ttf"},
		[]string{"Hack-Bold.ttf", "Hack-Regular.ttf"},
	)
	if !ok {
		t.Fatal("expected match")
	}
	if recase["Hack-Regular.ttf"] != "Hack-Regular.ttf" {
		t.Errorf("expected identity recase, got %q", recase["Hack-Regular.ttf"])
	}
}

func TestSameKeySetFold_CasingOnlyDiff(t *testing.T) {
	recase, ok := sameKeySetFold(
		[]string{"HackNerdFontMono-Regular.ttf"},
		[]string{"HackNerdFontMono-regular.ttf"},
	)
	if !ok {
		t.Fatal("expected a casing-only difference to still match")
	}
	if recase["HackNerdFontMono-regular.ttf"] != "HackNerdFontMono-Regular.ttf" {
		t.Errorf("expected recase to the newly discovered casing, got %q", recase["HackNerdFontMono-regular.ttf"])
	}
}

func TestSameKeySetFold_BinCasingOnlyDiff(t *testing.T) {
	recase, ok := sameKeySetFold(
		[]string{"bin/MyTool"},
		[]string{"bin/mytool"},
	)
	if !ok {
		t.Fatal("expected a casing-only bin rename to still match")
	}
	if recase["bin/mytool"] != "bin/MyTool" {
		t.Errorf("expected recase to the newly discovered casing, got %q", recase["bin/mytool"])
	}
}

func TestSameKeySetFold_ExtensionDiffers(t *testing.T) {
	_, ok := sameKeySetFold(
		[]string{"Hack-Regular.otf"},
		[]string{"Hack-Regular.ttf"},
	)
	if ok {
		t.Error("expected a real extension change to reprompt, not silently match")
	}
}

func TestSameKeySetFold_MembershipDiffers(t *testing.T) {
	_, ok := sameKeySetFold(
		[]string{"Hack-Regular.ttf", "Hack-Italic.ttf"},
		[]string{"Hack-Regular.ttf", "Hack-Bold.ttf"},
	)
	if ok {
		t.Error("expected a changed font set to reprompt")
	}
}

func TestInitCommand_Minimal(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})

	ci, err := initCommand(context.Background(), cmdOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ci.cfg == nil {
		t.Error("cfg is nil")
	}
	if ci.manifest != nil {
		t.Error("manifest should be nil without Manifest option")
	}
	if ci.unlock != nil {
		t.Error("unlock should be nil without Lock option")
	}
}

func TestInitCommand_WithManifest(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	writeManifest(t, &config.Manifest{
		Repos:    map[string]string{"fzf": "github.com/junegunn/fzf"},
		Extracts: map[string]config.PackageEntry{"fzf": {Version: "0.58.0"}},
	})

	ci, err := initCommand(context.Background(), cmdOptions{Manifest: true})
	if err != nil {
		t.Fatal(err)
	}
	if ci.manifest == nil {
		t.Fatal("manifest is nil")
	}
	if _, ok := ci.manifest.Extracts["fzf"]; !ok {
		t.Error("fzf not in manifest")
	}
}

func TestInitCommand_WithLock(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})

	ci, err := initCommand(context.Background(), cmdOptions{Lock: true})
	if err != nil {
		t.Fatal(err)
	}
	if ci.unlock == nil {
		t.Fatal("unlock should be set with Lock option")
	}
	ci.close()
}

func TestInitCommand_GHCheckFails(t *testing.T) {
	withHome(t)
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	writeSettings(t, &config.Settings{})

	// an already-expired context fails the vendor bootstrap's fetch without
	// touching the network, deterministically simulating "offline"
	ctx, cancel := context.WithDeadline(context.Background(), time.Now())
	defer cancel()

	_, err := initCommand(ctx, cmdOptions{GH: true})
	if err == nil {
		t.Fatal("expected error when gh not found")
	}
}

func TestInitCommand_ReposLoadFailure(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})

	ci, err := initCommand(context.Background(), cmdOptions{Repos: true})
	if err != nil {
		t.Fatal(err)
	}
	if ci.repos == nil {
		t.Error("repos should be empty map, not nil")
	}
}

func TestVerifyDigest_Match(t *testing.T) {
	content := []byte("hello ghpm")
	sum := sha256.Sum256(content)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	f, err := os.CreateTemp(t.TempDir(), "asset-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(content); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if err := verifyDigest(digest, f.Name()); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestVerifyDigest_Mismatch(t *testing.T) {
	digest := "sha256:" + hex.EncodeToString(make([]byte, 32))
	f, err := os.CreateTemp(t.TempDir(), "asset-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("wrong content")); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if err := verifyDigest(digest, f.Name()); err == nil {
		t.Error("expected mismatch error, got nil")
	}
}

func TestVerifyDigest_BadFormat(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "asset-*")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if err := verifyDigest("md5:abc123", f.Name()); err == nil {
		t.Error("expected error for unsupported algorithm, got nil")
	}
	if err := verifyDigest("nodivider", f.Name()); err == nil {
		t.Error("expected error for missing colon, got nil")
	}
}

func TestInitCommand_SkipHashCheck_PropagatesFromSettings(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{SkipHashCheck: true})
	skipHashCheck = false
	defer func() { skipHashCheck = false }()

	_, err := initCommand(context.Background(), cmdOptions{SkipHashCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	if !skipHashCheck {
		t.Error("skipHashCheck should be true when settings say SkipHashCheck")
	}
}

// TestPrintFailedTable is the regression test for a real report: a run that
// hit errors on two packages (pwsh and deno, both busy/locked at the time)
// still ended with just "✓ installed N package(s)" and nothing else — the
// earlier inline ✗ lines naming why had scrolled out of view by then, so the
// summary alone looked like a clean success and finding the reason meant
// scrolling back up. printFailedTable is the shared closing block every
// mutating command now calls: a table of name + reason, right next to the
// success tally, so the reason is still on screen when the run ends — no
// scrolling required. It runs *alongside* the inline per-failure printFail
// (a typical build-tool pattern: report as it happens, then summarize at the
// end), not instead of it — the two aren't the close-proximity duplication
// this app avoids elsewhere, since they're usually screens apart.
func TestPrintFailedTable(t *testing.T) {
	t.Run("no items, no output at all", func(t *testing.T) {
		var buf bytes.Buffer
		ui.SetOutput(&buf)
		t.Cleanup(func() { ui.SetOutput(os.Stdout) })

		printFailedTable(nil, "package", nil)

		if got := buf.String(); got != "" {
			t.Errorf("got %q, want empty output", got)
		}
	})

	t.Run("renders a name/reason table, sorted by name", func(t *testing.T) {
		var buf bytes.Buffer
		ui.SetOutput(&buf)
		t.Cleanup(func() { ui.SetOutput(os.Stdout) })

		printFailedTable(nil, "package", []failedItem{
			{name: "pwsh", reason: "resource busy"},
			{name: "deno", reason: "file locked"},
		})

		out := buf.String()
		if !strings.Contains(out, "✗ 2 package(s) failed") {
			t.Errorf("expected a failed-count header, got:\n%s", out)
		}
		if !strings.Contains(out, "name") || !strings.Contains(out, "reason") {
			t.Errorf("expected a name/reason table, got:\n%s", out)
		}
		if !strings.Contains(out, "deno") || !strings.Contains(out, "file locked") {
			t.Errorf("expected deno's reason in the table, got:\n%s", out)
		}
		if !strings.Contains(out, "pwsh") || !strings.Contains(out, "resource busy") {
			t.Errorf("expected pwsh's reason in the table, got:\n%s", out)
		}
		if strings.Index(out, "deno") > strings.Index(out, "pwsh") {
			t.Errorf("expected rows sorted by name (deno before pwsh), got:\n%s", out)
		}
	})

	t.Run("blank line before when something already printed", func(t *testing.T) {
		var buf bytes.Buffer
		ui.SetOutput(&buf)
		t.Cleanup(func() { ui.SetOutput(os.Stdout) })

		printPass(nil, "installed 1 package(s)")
		printFailedTable(nil, "package", []failedItem{{name: "deno", reason: "file locked"}})

		out := buf.String()
		if !strings.Contains(out, "package(s)\n\n✗ 1 package(s) failed") {
			t.Errorf("expected a blank line separating the two sections, got:\n%s", out)
		}
	})

	t.Run("no leading blank when it's the first thing printed", func(t *testing.T) {
		var buf bytes.Buffer
		ui.SetOutput(&buf)
		t.Cleanup(func() { ui.SetOutput(os.Stdout) })

		printFailedTable(nil, "package", []failedItem{{name: "deno", reason: "file locked"}})

		out := buf.String()
		if strings.HasPrefix(out, "\n") {
			t.Errorf("got %q, unexpected leading blank line on the very first output", out)
		}
	})

	t.Run("blank line after when more output follows", func(t *testing.T) {
		var buf bytes.Buffer
		ui.SetOutput(&buf)
		t.Cleanup(func() { ui.SetOutput(os.Stdout) })

		printFailedTable(nil, "package", []failedItem{{name: "deno", reason: "file locked"}})
		print("next section")

		out := buf.String()
		if !strings.HasSuffix(out, "\n\nnext section\n") {
			t.Errorf("got %q, expected a blank line before the next section", out)
		}
	})

	t.Run("no trailing blank when it's the last thing printed", func(t *testing.T) {
		var buf bytes.Buffer
		ui.SetOutput(&buf)
		t.Cleanup(func() { ui.SetOutput(os.Stdout) })

		printFailedTable(nil, "package", []failedItem{{name: "deno", reason: "file locked"}})

		out := buf.String()
		if strings.HasSuffix(out, "\n\n") {
			t.Errorf("got %q, unexpected trailing blank line", out)
		}
	})
}

func TestInitCommand_WithDirs(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})

	ci, err := initCommand(context.Background(), cmdOptions{Dirs: true})
	if err != nil {
		t.Fatal(err)
	}
	binDir, err := store.BinDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(binDir); os.IsNotExist(err) {
		t.Error("bin dir was not created")
	}
	_ = ci
}
