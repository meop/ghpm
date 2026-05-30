package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/store"
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
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), append(data, '\n'), 0644); err != nil {
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
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func TestReservedShimNames_ExcludesOwner(t *testing.T) {
	manifest := &config.Manifest{
		Extracts: map[string]config.PackageEntry{
			"fzf": {Asset: map[string]config.AssetEntry{
				"fzf.tar.gz": {Bin: map[string]string{"fzf": "fzf"}},
			}},
			"uv": {Asset: map[string]config.AssetEntry{
				"uv.tar.gz": {Bin: map[string]string{"uv": "uv", "uvx": "uvx"}},
			}},
			"uv@0.7": {Asset: map[string]config.AssetEntry{
				"uv.tar.gz": {Bin: map[string]string{"uv@0.7": "uv"}},
			}},
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
			"nerd-fonts": {Asset: map[string]config.AssetEntry{
				"Hack.zip": {Font: map[string]string{"hack": "Hack-Regular.ttf"}},
			}},
			"other-fonts": {Asset: map[string]config.AssetEntry{
				"Mono.zip": {Font: map[string]string{"mono": "Mono.ttf"}},
			}},
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

func TestInitCommand_Minimal(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})

	ci, err := initCommand(cmdOptions{})
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

	ci, err := initCommand(cmdOptions{Manifest: true})
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

	ci, err := initCommand(cmdOptions{Lock: true})
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

	_, err := initCommand(cmdOptions{GH: true})
	if err == nil {
		t.Fatal("expected error when gh not found")
	}
}

func TestInitCommand_ReposLoadFailure(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})

	ci, err := initCommand(cmdOptions{Repos: true})
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

	_, err := initCommand(cmdOptions{SkipHashCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	if !skipHashCheck {
		t.Error("skipHashCheck should be true when settings say SkipHashCheck")
	}
}

func TestInitCommand_WithDirs(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})

	ci, err := initCommand(cmdOptions{Dirs: true})
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
