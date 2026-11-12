// Package ghapi is ghpm's one way to GitHub's API: go-gh's HTTP client, with
// ghpm's own token, and never gh's config, keyring or binary.
//
// A token is GH_TOKEN, then GITHUB_TOKEN, then the one `ghpm auth login`
// kept in ~/.ghpm/token. With none, or with one GitHub turns away, a terminal
// is asked for a new one, once; without a terminal the error says how to give
// one.
package ghapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/meop/golie/ui"

	"github.com/meop/ghpm/internal/store"
)

// Where requests go. Vars, not consts, so a test can point them at a local
// server: the token is sent only to Host, its subdomains, and APIHost.
var (
	BaseURL = "https://api.github.com/"
	Host    = "github.com"
	APIHost = "api.github.com"
)

var (
	ErrRateLimited = errors.New("rate limited")
	ErrAuthFailed  = errors.New("GitHub turned the token away")
	ErrNotFound    = errors.New("not found")
	ErrNoToken     = errors.New("no GitHub token: run `ghpm auth login`, or set GH_TOKEN")
)

// TokenPrompt is what a terminal is asked when ghpm needs a token.
const TokenPrompt = "ghpm needs a GitHub token (a fine-grained PAT with Contents: Read-only is enough): paste one, then press enter: "

// Source says where a token came from: an environment variable's name, or
// the token file's path.
type Source string

// Token is the token ghpm uses, and where it came from; "" for both when
// there is none.
func Token() (string, Source) {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if tok := strings.TrimSpace(os.Getenv(name)); tok != "" {
			return tok, Source(name)
		}
	}
	path, err := store.TokenFile()
	if err != nil {
		return "", ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	if tok := strings.TrimSpace(string(data)); tok != "" {
		return tok, Source(path)
	}
	return "", ""
}

// FromEnv reports whether src is an environment variable rather than the
// token file, which ghpm does not rewrite.
func (src Source) FromEnv() bool { return src == "GH_TOKEN" || src == "GITHUB_TOKEN" }

// SaveToken writes tok to the token file, readable by its owner alone, by
// rename so a reader never sees half of it.
func SaveToken(tok string) (string, error) {
	path, err := store.TokenFile()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".token-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if _, err := tmp.WriteString(strings.TrimSpace(tok) + "\n"); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return path, os.Rename(tmp.Name(), path)
}

// RemoveToken deletes the token file, and says whether there was one.
func RemoveToken() (bool, error) {
	path, err := store.TokenFile()
	if err != nil {
		return false, err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

// User is the login tok belongs to, as GitHub says: the check `auth login`
// makes before keeping a token, and `auth status` makes of the one in use.
func User(ctx context.Context, tok string) (string, error) {
	var u struct {
		Login string `json:"login"`
	}
	resp, err := send(ctx, tok, 0, http.MethodGet, BaseURL+"user", nil, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return "", fmt.Errorf("reading user: %w", err)
	}
	return u.Login, nil
}

// GetJSON decodes what GitHub answers a GET of path (relative to BaseURL) into
// out.
func GetJSON(ctx context.Context, path string, out any) error {
	resp, err := Do(ctx, http.MethodGet, path, nil, nil, 0)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	return nil
}

// GetRaw is the body GitHub answers a GET of path with, asked for as accept.
func GetRaw(ctx context.Context, path, accept string) ([]byte, error) {
	resp, err := Do(ctx, http.MethodGet, path, nil, map[string]string{"Accept": accept}, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

// GraphQL posts query and gives back the response body whole: a batch's
// partial errors are in it, for the caller to read beside the data. A ttl
// above zero answers from ghpm's cache while an answer to the same query is
// younger than it.
func GraphQL(ctx context.Context, query string, ttl time.Duration) ([]byte, error) {
	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return nil, err
	}
	resp, err := Do(ctx, http.MethodPost, "graphql", body, map[string]string{"Content-Type": "application/json"}, ttl)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

// Download writes the release asset with this API path to w. GitHub answers
// with a redirect to its storage, which the client follows without the token.
func Download(ctx context.Context, path string, w io.Writer) error {
	resp, err := Do(ctx, http.MethodGet, path, nil, map[string]string{"Accept": "application/octet-stream"}, 0)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, err = io.Copy(w, resp.Body)
	return err
}

// EnsureToken asks a terminal for a token when there is none, before any
// request: a command calls it up front, so the question comes from its own
// goroutine rather than from a parallel worker's request.
func EnsureToken() error {
	if tok, _ := Token(); tok != "" {
		return nil
	}
	_, err := reprompt("")
	return err
}

// promptMu keeps two requests failing at once from asking for a token twice.
var promptMu sync.Mutex

// Do sends a request with ghpm's token, and gives back a response GitHub
// answered with success; anything else is an error, ErrAuthFailed,
// ErrRateLimited or ErrNotFound where it is one of those. A missing or
// refused token is asked for again at a terminal, once, and the request
// retried with the new one.
func Do(ctx context.Context, method, path string, body []byte, headers map[string]string, ttl time.Duration) (*http.Response, error) {
	url := path
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		url = BaseURL + strings.TrimPrefix(path, "/")
	}
	tok, src := Token()
	if tok != "" {
		resp, err := send(ctx, tok, ttl, method, url, body, headers)
		if err == nil || !errors.Is(err, ErrAuthFailed) || src.FromEnv() {
			return resp, err
		}
	}
	tok, err := reprompt(tok)
	if err != nil {
		return nil, err
	}
	return send(ctx, tok, ttl, method, url, body, headers)
}

// reprompt asks a terminal for a token to replace failed ("" when there was
// none), keeps it, and gives it back. A request that waited on another's
// prompt takes the token that one kept.
func reprompt(failed string) (string, error) {
	promptMu.Lock()
	defer promptMu.Unlock()
	if tok, _ := Token(); tok != "" && tok != failed {
		return tok, nil
	}
	if !ui.Interactive() {
		if failed != "" {
			return "", fmt.Errorf("%w: run `ghpm auth login` for a new one", ErrAuthFailed)
		}
		return "", ErrNoToken
	}
	tok, err := ui.ReadSecret(TokenPrompt)
	if err != nil {
		return "", err
	}
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return "", ErrNoToken
	}
	if _, err := SaveToken(tok); err != nil {
		return "", err
	}
	return tok, nil
}

// clients are go-gh HTTP clients by token and cache ttl, made once each.
var (
	clientsMu sync.Mutex
	clients   = map[[3]string]*http.Client{}
)

func client(tok string, ttl time.Duration) (*http.Client, error) {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	cacheDir, err := store.HTTPCacheDir()
	if err != nil {
		return nil, err
	}
	key := [3]string{tok, ttl.String(), cacheDir}
	if c, ok := clients[key]; ok {
		return c, nil
	}
	// Host, APIHost, AuthToken and Transport are all given, so go-gh reads
	// nothing of gh's own config
	c, err := api.NewHTTPClient(api.ClientOptions{
		Host:         Host,
		APIHost:      APIHost,
		AuthToken:    tok,
		Transport:    http.DefaultTransport,
		EnableCache:  ttl > 0,
		CacheTTL:     ttl,
		CacheDir:     cacheDir,
		LogIgnoreEnv: true,
		Headers:      map[string]string{"Accept": "application/vnd.github+json", "User-Agent": "ghpm"},
	})
	if err != nil {
		return nil, err
	}
	clients[key] = c
	return c, nil
}

func send(ctx context.Context, tok string, ttl time.Duration, method, url string, body []byte, headers map[string]string) (*http.Response, error) {
	c, err := client(tok, ttl)
	if err != nil {
		return nil, err
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 300 {
		return resp, nil
	}
	defer func() { _ = resp.Body.Close() }()
	return nil, statusError(resp)
}

// statusError says what a failed response means, by its status and GitHub's
// message.
func statusError(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var msg struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(data, &msg)
	text := msg.Message
	if text == "" {
		text = strings.TrimSpace(string(data))
	}
	if text == "" {
		text = http.StatusText(resp.StatusCode)
	}
	what := fmt.Sprintf("HTTP %d: %s (%s)", resp.StatusCode, text, resp.Request.URL.Path)
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", ErrAuthFailed, what)
	case resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode == http.StatusForbidden && (resp.Header.Get("X-RateLimit-Remaining") == "0" || strings.Contains(strings.ToLower(text), "rate limit")):
		return fmt.Errorf("%w: %s", ErrRateLimited, what)
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, what)
	}
	return errors.New(what)
}
