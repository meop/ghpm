package ghbin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/meop/ghpm/internal/fetch"
	"github.com/meop/ghpm/internal/toolchain"
	"github.com/meop/ghpm/internal/ui"
)

// ghReleaseBase is where gh's releases are downloaded from: a plain release
// download addressed by the pinned version, never "latest" and never the
// GitHub API, so there is no token involved and no API rate limit to meet
// before gh exists to carry one. It is a var, not a const, so a test can
// point it at a local server.
var ghReleaseBase = "https://github.com/cli/cli/releases/download/"

// Ensure makes sure ghpm has its own vendored gh at exactly toolchain.GhVersion,
// downloading that release directly — gh is obviously not available yet to
// fetch gh with. Never skipped just because something is on PATH: ghpm's own
// gh use doesn't depend on it. The check is the vendored copy's *version*,
// not merely its presence, so a copy left behind by an older or newer ghpm is
// replaced rather than run; an upgrade and a downgrade are the same
// operation. It costs one local `gh --version` per invocation and no network
// at all while the pin holds. Staying *authenticated* is a separate concern
// handled reactively — see ReAuth — rather than checked here on every
// invocation: that would mean a network round trip before every single gh
// call, on top of the one the call itself is about to make.
func Ensure(ctx context.Context) error {
	path, err := VendorPath()
	if err != nil {
		return err
	}
	if toolchain.Installed(path) == toolchain.GhVersion {
		return nil
	}
	if err := bootstrap(ctx, path); err != nil {
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

// bootstrap fetches the pinned gh release for this platform, checks it
// against the digest pinned with the version, and puts its gh at dest. The
// archive is unpacked in a work dir beside dest that is removed whatever
// happens, and the old binary is dropped before the new one moves in, so the
// vendor dir ends up holding exactly one gh and nothing left over from the
// last one. gh's config dir beside it — ghpm's token — is never touched.
func bootstrap(ctx context.Context, dest string) error {
	name, sha256, ok := toolchain.GhAsset(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return fmt.Errorf("no gh release available for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	vendorDir := filepath.Dir(dest)
	if err := os.MkdirAll(vendorDir, 0755); err != nil {
		return err
	}
	work, err := os.MkdirTemp(vendorDir, ".gh-bootstrap-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()

	archive := filepath.Join(work, name)
	if err := fetch.Download(ctx, ghReleaseBase+"v"+toolchain.GhVersion+"/"+name, archive, sha256); err != nil {
		return fmt.Errorf("fetching gh %s: %w", toolchain.GhVersion, err)
	}
	unpacked := filepath.Join(work, "unpacked")
	if err := fetch.Extract(archive, unpacked, 0); err != nil {
		return err
	}
	extracted, err := findFile(unpacked, vendorName())
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	// dest may already hold a differently-versioned gh; drop it first so the
	// rename isn't replacing a file another process may still hold open.
	_ = os.Remove(dest)
	if err := os.Rename(extracted, dest); err != nil {
		return err
	}
	return os.Chmod(dest, 0755)
}

// findFile returns the path of the file called name somewhere under dir.
func findFile(dir, name string) (string, error) {
	var found string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == name {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("%s not found in the archive", name)
	}
	return found, nil
}
