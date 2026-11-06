package faketool

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Release stands up a release page as GitHub serves one: "latest" redirects
// to the tag "v"+version, and download/v<version>/<name> serves each of
// files. It returns the page's URL with a trailing slash, and a count of the
// downloads of any file but sums.
func Release(t testing.TB, version string, files map[string][]byte, sums string) (string, *int) {
	t.Helper()
	fetches := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/tag/v"+version, http.StatusFound)
	})
	for name, body := range files {
		mux.HandleFunc("/download/v"+version+"/"+name, func(w http.ResponseWriter, r *http.Request) {
			if name != sums {
				fetches++
			}
			_, _ = w.Write(body)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL + "/", &fetches
}

// Sums is a checksum manifest, as sha256sum writes one, listing each of files.
func Sums(files map[string][]byte) []byte {
	var out []byte
	for name, body := range files {
		s := sha256.Sum256(body)
		out = fmt.Appendf(out, "%s  %s\n", hex.EncodeToString(s[:]), name)
	}
	return out
}

// Unreachable is a URL with nothing listening, with a trailing slash: a test
// that points a release page at it proves nothing asked that page anything,
// since any request would fail.
func Unreachable(t testing.TB) string {
	t.Helper()
	srv := httptest.NewServer(nil)
	url := srv.URL + "/"
	srv.Close()
	return url
}
