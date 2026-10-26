package ghbin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/meop/golie/pinned"
	"github.com/meop/golie/ui"

	"github.com/meop/ghpm/internal/toolchain"
)

// Ensure makes sure ghpm has its own vendored gh, and that it is exactly the
// release toolchain pins: not merely a gh at that version, but the pinned
// binary, checked by its sha256 on every call. Never skipped just because
// something is on PATH: ghpm's own gh use doesn't depend on it. A copy from
// another version, a damaged one and one something else put there all fail
// the same check and are replaced the same way — a plain download of the
// pinned release, checked before it is installed (see golie's pinned
// package). It costs one hash of gh per invocation, which is less than
// asking gh its version was, and no network at all while the pin holds.
// Staying *authenticated* is a separate concern handled reactively — see
// ReAuth — rather than checked here on every invocation: that would mean a
// network round trip before every single gh call, on top of the one the call
// itself is about to make.
func Ensure(ctx context.Context) error {
	dir, err := vendorSubDir()
	if err != nil {
		return err
	}
	tool, ok := toolchain.Gh(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return fmt.Errorf("no gh release available for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if tool.Verified(dir) {
		return nil
	}
	// gh's config dir sits beside it and holds ghpm's token: it is kept, and
	// everything else that is not the pinned gh is cleared out. Silent: it is
	// internals, not something its user asked for
	err = pinned.Ensure(ctx, tool, dir, pinned.Options{Keep: []string{configDirName}})
	if err != nil {
		return err
	}
	path, err := VendorPath()
	if err != nil {
		return err
	}
	return authIfNeeded(ctx, path)
}

// authIfNeeded runs `gh auth login` against a freshly vendored gh when
// nobody's logged in yet — the vendored copy starts with an empty config, and
// gh refuses almost everything until it has a token. Skipped entirely when
// there's nobody to answer the prompt; that command's own gh invocation will
// then fail with gh's own auth-login message.
func authIfNeeded(ctx context.Context, ghPath string) error {
	if !ui.Interactive() {
		return nil
	}
	cfgDir, err := ConfigDir()
	if err != nil {
		return err
	}
	env := append(os.Environ(), "GH_CONFIG_DIR="+cfgDir)

	status := exec.CommandContext(ctx, ghPath, "auth", "status")
	status.Env = env
	if status.Run() == nil {
		return nil
	}
	return login(ctx, ghPath, env)
}

// login prompts for a personal access token rather than the browser device
// flow: ghpm's vendored gh is meant for unattended use, and a device flow's
// short-lived token has no way to silently renew itself once nothing is
// watching for the prompt that would refresh it. --insecure-storage keeps the
// token in ghpm's own GH_CONFIG_DIR instead of the OS credential store: ghpm
// is often run over SSH, where the keyring paths gh otherwise uses (a desktop
// session's credential helper) are unavailable or locked. Reads the token itself
// (ui.ReadSecret) rather than handing gh the raw terminal — piping os.Stdin
// straight through relies on the person typing to send a clean EOF
// themselves, which is exactly the kind of thing that just hangs.
func login(ctx context.Context, ghPath string, env []string) error {
	token, err := ui.ReadSecret("ghpm's gh needs a token (fine-grained PAT, Contents: Read-only) — paste one, then press enter: ")
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, ghPath, "auth", "login", "--insecure-storage", "--with-token")
	cmd.Env = env
	cmd.Stdin = strings.NewReader(token)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ReAuth is the reactive half of auth handling: called when a gh invocation
// has just failed with what looks like a bad or expired token (see
// gh.isAuthError), it re-prompts for a token — interactively only, same as
// authIfNeeded — so the failing command can retry once instead of leaving a
// dead credential to fail every command after it too.
func ReAuth(ctx context.Context) error {
	if !ui.Interactive() {
		return fmt.Errorf("gh authentication failed — run any ghpm command interactively to re-authenticate")
	}
	path, err := VendorPath()
	if err != nil {
		return err
	}
	cfgDir, err := ConfigDir()
	if err != nil {
		return err
	}
	env := append(os.Environ(), "GH_CONFIG_DIR="+cfgDir)
	return login(ctx, path, env)
}
