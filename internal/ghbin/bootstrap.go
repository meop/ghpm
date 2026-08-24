package ghbin

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/meop/ghpm/internal/ui"
)

// ghReleaseAPI is a var, not a const, so a test can point it at a local
// server instead of the real GitHub API.
var ghReleaseAPI = "https://api.github.com/repos/cli/cli/releases/latest"

type ghRelease struct {
	Assets []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// Ensure makes sure ghpm has its own vendored gh, downloading the latest
// release directly — gh is obviously not available yet to fetch gh with —
// when nothing is vendored yet. Never skipped just because something is on
// PATH: ghpm's own gh use doesn't depend on it. A no-op once a vendored copy
// exists; staying current after that is `ghpm upgrade`'s job, not every
// invocation's.
func Ensure(ctx context.Context) error {
	path, err := VendorPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
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
// there's nobody to answer the prompts; that command's own gh invocation will
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

	ui.Out("Authenticating ghpm's gh...")
	login := exec.CommandContext(ctx, ghPath, "auth", "login", "--insecure-storage")
	login.Env = env
	login.Stdin = os.Stdin
	login.Stdout = os.Stdout
	login.Stderr = os.Stderr
	return login.Run()
}

func bootstrap(ctx context.Context, dest string) error {
	pattern, ext := ghAssetPattern()
	if pattern == "" {
		return fmt.Errorf("no gh release available for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	var rel ghRelease
	if err := fetchJSON(ctx, ghReleaseAPI, &rel); err != nil {
		return fmt.Errorf("fetching latest gh release: %w", err)
	}
	var assetURL, assetName string
	for _, a := range rel.Assets {
		if strings.Contains(a.Name, pattern) && strings.HasSuffix(a.Name, ext) {
			assetURL, assetName = a.BrowserDownloadURL, a.Name
			break
		}
	}
	if assetURL == "" {
		return fmt.Errorf("no gh release asset matches %q", pattern+"*"+ext)
	}

	vendorDir := filepath.Dir(dest)
	if err := os.MkdirAll(vendorDir, 0755); err != nil {
		return err
	}
	work, err := os.MkdirTemp(vendorDir, ".gh-bootstrap-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	archivePath := filepath.Join(work, assetName)
	if err := downloadFile(ctx, assetURL, archivePath); err != nil {
		return fmt.Errorf("downloading %s: %w", assetName, err)
	}

	extracted, err := extractOne(archivePath, vendorName())
	if err != nil {
		return err
	}
	if err := os.Rename(extracted, dest); err != nil {
		return err
	}
	return os.Chmod(dest, 0755)
}

func ghAssetPattern() (pattern, ext string) {
	switch runtime.GOOS {
	case "linux":
		return "linux_" + runtime.GOARCH, ".tar.gz"
	case "darwin":
		return "macOS_" + runtime.GOARCH, ".zip"
	case "windows":
		return "windows_" + runtime.GOARCH, ".zip"
	default:
		return "", ""
	}
}

func fetchJSON(ctx context.Context, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func downloadFile(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

// extractOne pulls a single named file out of archivePath (.tar.gz or .zip —
// the only two formats gh ships) and writes it alongside the archive.
func extractOne(archivePath, wantName string) (string, error) {
	out := filepath.Join(filepath.Dir(archivePath), wantName)
	switch lower := strings.ToLower(archivePath); {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return out, extractOneFromTarGz(archivePath, wantName, out)
	case strings.HasSuffix(lower, ".zip"):
		return out, extractOneFromZip(archivePath, wantName, out)
	default:
		return "", fmt.Errorf("unsupported archive format: %s", archivePath)
	}
}

func extractOneFromTarGz(archivePath, wantName, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == wantName {
			return streamToFile(tr, dest)
		}
	}
	return fmt.Errorf("%s not found in %s", wantName, filepath.Base(archivePath))
}

func extractOneFromZip(archivePath, wantName, dest string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.FileInfo().IsDir() || filepath.Base(f.Name) != wantName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		return streamToFile(rc, dest)
	}
	return fmt.Errorf("%s not found in %s", wantName, filepath.Base(archivePath))
}

func streamToFile(r io.Reader, dest string) error {
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, r)
	return err
}
