package ghbin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/meop/golie/ui"
	"github.com/meop/golie/vendored"

	"github.com/meop/ghpm/internal/toolchain"
)

// Ensure makes sure ghpm has its own vendored gh. Never skipped just because
// something is on PATH: ghpm's own gh use doesn't depend on it. Whatever
// release is there is used, with no network; only where there is none — a new
// machine, a removed vendor dir — is the latest fetched, checked against its
// release's checksums first (see golie's vendored package), and logged in.
// fetching, when not nil, is told the archive just before that download.
// Staying *authenticated* is a separate concern handled reactively — see
// ReAuth — rather than checked here on every invocation: that would mean a
// network round trip before every single gh call, on top of the one the call
// itself is about to make.
func Ensure(ctx context.Context, fetching func(archive string)) error {
	dir, r, err := Release()
	if err != nil {
		return err
	}
	installed, err := r.Ensure(ctx, dir, options(fetching))
	if err != nil || installed == "" {
		return err
	}
	return loginFresh(ctx)
}

// Refresh moves ghpm's vendored gh to gh's latest release, and says what it
// had and what it has: the same twice when it was current, and from "" when
// there was none.
func Refresh(ctx context.Context, fetching func(archive string)) (from, to string, err error) {
	dir, r, err := Release()
	if err != nil {
		return "", "", err
	}
	from, to, err = r.Refresh(ctx, dir, options(fetching))
	if err != nil || from != "" || to == "" {
		return from, to, err
	}
	return from, to, loginFresh(ctx)
}

// Release is ghpm's vendored gh: the dir it is kept in, and gh's release for
// this machine.
func Release() (string, vendored.Release, error) {
	dir, err := vendorSubDir()
	if err != nil {
		return "", vendored.Release{}, err
	}
	r, ok := toolchain.Gh(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return "", vendored.Release{}, fmt.Errorf("no gh release available for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return dir, r, nil
}

// options keeps gh's config dir, which sits beside it and holds ghpm's token;
// everything else that is not gh is cleared out
func options(fetching func(string)) vendored.Options {
	return vendored.Options{Keep: []string{configDirName}, Fetching: fetching}
}

// loginFresh logs a just-vendored gh in, when it is not already
func loginFresh(ctx context.Context) error {
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
