// Package fakegh is a GitHub API a test stands up in place of the real one:
// releases and their assets, repo contents, repository search and GraphQL,
// answered from what the test puts in it. New points ghpm's API client at it
// for the rest of the test, with a token it takes.
package fakegh

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/meop/ghpm/internal/ghapi"
)

// Token is the token the fake takes; any other is turned away with a 401.
const Token = "fakegh-token"

// Asset is one file of a release.
type Asset struct {
	Name   string
	Body   []byte
	Digest string
}

// Release is one release of a repo, newest first in Server.Releases.
type Release struct {
	Tag        string
	Prerelease bool
	Assets     []Asset
}

// Hit is one answer repository search gives.
type Hit struct {
	FullName, Description string
}

// Server is the fake. Its fields may be set before and between requests.
type Server struct {
	*httptest.Server

	mu sync.Mutex
	// Releases are each repo's ("owner/repo"), newest first.
	Releases map[string][]Release
	// Latest is a repo's latest release tag where it is not its first stable one.
	Latest map[string]string
	// Files are each repo's files at its root, by name.
	Files map[string]map[string][]byte
	// Search answers a repository search for q ("<term> in:<fields>").
	Search func(q string) []Hit
	// GraphQL answers a POST to /graphql with a status and a body.
	GraphQL func(query string) (int, string)
	// Fail, when set, answers any request it returns a status above zero for,
	// with that status and body, before anything else does.
	Fail  func(r *http.Request) (int, string)
	calls []string
	ids   map[int64]Asset
	idOf  map[string]int64
}

// New starts a fake GitHub for this test, and points ghpm at it with its
// token in GH_TOKEN.
func New(t *testing.T) *Server {
	t.Helper()
	s := &Server{Releases: map[string][]Release{}, Latest: map[string]string{}, Files: map[string]map[string][]byte{}, ids: map[int64]Asset{}, idOf: map[string]int64{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	base, host := ghapi.BaseURL, ghapi.APIHost
	ghapi.BaseURL = s.URL + "/"
	ghapi.APIHost = "127.0.0.1"
	t.Cleanup(func() { ghapi.BaseURL, ghapi.APIHost = base, host })
	t.Setenv("GH_TOKEN", Token)
	t.Setenv("GITHUB_TOKEN", "")
	return s
}

// Calls are the requests made so far, as "METHOD /path".
func (s *Server) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

// AddRelease puts a release at the front of repo's, as the newest.
func (s *Server) AddRelease(repo string, r Release) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Releases[repo] = append([]Release{r}, s.Releases[repo]...)
}

// BlobSHA is the hash GitHub gives a file with this content.
func BlobSHA(data []byte) string {
	h := sha1.New()
	_, _ = fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, r.Method+" "+r.URL.Path)
	if s.Fail != nil {
		if code, body := s.Fail(r); code > 0 {
			w.WriteHeader(code)
			_, _ = io.WriteString(w, body)
			return
		}
	}
	if r.Header.Get("Authorization") != "token "+Token {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/graphql":
		var body struct{ Query string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		if s.GraphQL == nil {
			writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{}})
			return
		}
		code, out := s.GraphQL(body.Query)
		w.WriteHeader(code)
		_, _ = io.WriteString(w, out)
	case r.URL.Path == "/user":
		writeJSON(w, http.StatusOK, map[string]string{"login": "tester"})
	case r.URL.Path == "/search/repositories":
		var hits []Hit
		if s.Search != nil {
			hits = s.Search(r.URL.Query().Get("q"))
		}
		items := make([]map[string]string, len(hits))
		for i, h := range hits {
			items[i] = map[string]string{"full_name": h.FullName, "description": h.Description}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case len(parts) >= 4 && parts[0] == "repos" && parts[3] == "contents":
		s.serveContents(w, r, parts[1]+"/"+parts[2], parts[4:])
	case len(parts) >= 4 && parts[0] == "repos" && parts[3] == "releases":
		s.serveReleases(w, r, parts[1]+"/"+parts[2], parts[4:])
	default:
		notFound(w)
	}
}

func (s *Server) serveContents(w http.ResponseWriter, r *http.Request, repo string, rest []string) {
	files, ok := s.Files[repo]
	if !ok {
		notFound(w)
		return
	}
	if len(rest) == 0 {
		var list []map[string]string
		for name, data := range files {
			list = append(list, map[string]string{"path": name, "sha": BlobSHA(data)})
		}
		writeJSON(w, http.StatusOK, list)
		return
	}
	data, ok := files[strings.Join(rest, "/")]
	if !ok {
		notFound(w)
		return
	}
	_, _ = w.Write(data)
}

func (s *Server) serveReleases(w http.ResponseWriter, r *http.Request, repo string, rest []string) {
	releases := s.Releases[repo]
	switch {
	case len(rest) == 0:
		perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if perPage <= 0 {
			perPage = 30
		}
		if page <= 0 {
			page = 1
		}
		var out []map[string]any
		for i := (page - 1) * perPage; i < len(releases) && i < page*perPage; i++ {
			out = append(out, s.render(repo, i, releases[i]))
		}
		if out == nil {
			out = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, out)
	case len(rest) == 1 && rest[0] == "latest":
		tag := s.Latest[repo]
		for i, rel := range releases {
			if (tag == "" && !rel.Prerelease) || rel.Tag == tag {
				writeJSON(w, http.StatusOK, s.render(repo, i, rel))
				return
			}
		}
		notFound(w)
	case len(rest) == 2 && rest[0] == "tags":
		tag, _ := url.PathUnescape(rest[1])
		for i, rel := range releases {
			if rel.Tag == tag {
				writeJSON(w, http.StatusOK, s.render(repo, i, rel))
				return
			}
		}
		notFound(w)
	case len(rest) == 2 && rest[0] == "assets":
		id, _ := strconv.ParseInt(rest[1], 10, 64)
		a, ok := s.ids[id]
		if !ok || r.Header.Get("Accept") != "application/octet-stream" {
			notFound(w)
			return
		}
		_, _ = w.Write(a.Body)
	default:
		notFound(w)
	}
}

// render is a release as GitHub's REST API answers it, its assets given ids
// a download can name
func (s *Server) render(repo string, i int, rel Release) map[string]any {
	assets := make([]map[string]any, len(rel.Assets))
	for j, a := range rel.Assets {
		key := repo + "@" + rel.Tag + "/" + a.Name
		id, ok := s.idOf[key]
		if !ok {
			id = int64(len(s.idOf) + 1)
			s.idOf[key] = id
		}
		s.ids[id] = a
		assets[j] = map[string]any{
			"id": id, "name": a.Name, "size": len(a.Body), "digest": a.Digest,
			"browser_download_url": fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, rel.Tag, a.Name),
		}
	}
	return map[string]any{"tag_name": rel.Tag, "prerelease": rel.Prerelease, "draft": false, "assets": assets}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
}
