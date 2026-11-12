package cli

import (
	"bytes"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/fakegh"
	"github.com/meop/golie/ui"
)

// TestRunInfo_OrderPreservedAcrossFailureModes exercises info's three phases
// (local resolution, parallel fetch, sequential print) with one arg failing
// locally (before any network call), one succeeding, and one failing during
// the network fetch — and asserts the printed blocks still land in original
// argument order even though the fetch phase completes out of order.
func TestRunInfo_OrderPreservedAcrossFailureModes(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	writeManifest(t, &config.Manifest{
		Repos: map[string]string{
			"goodpkg":    "github.com/good/repo",
			"badnetwork": "github.com/bad/repo",
		},
	})

	gh := fakeAPI(t)
	gh.AddRelease("good/repo", fakegh.Release{Tag: "v1.0.0", Assets: []fakegh.Asset{{Name: "good-1.0.0.tar.gz", Body: []byte("x")}}})
	gh.Fail = func(r *http.Request) (int, string) {
		if strings.HasPrefix(r.URL.Path, "/repos/bad/repo/") {
			return 502, `{"message":"boom from bad repo"}`
		}
		return 0, ""
	}

	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	err := runInfo(cmdWithContext(), []string{"bad/name", "goodpkg", "badnetwork"})
	if err == nil {
		t.Fatal("expected an error given two of the three args fail")
	}

	out := buf.String()
	idxBadName := strings.Index(out, "info: bad/name")
	idxGood := strings.Index(out, "info: goodpkg")
	idxBadNet := strings.Index(out, "info: badnetwork")
	if idxBadName < 0 || idxGood < 0 || idxBadNet < 0 {
		t.Fatalf("expected all three headers present:\n%s", out)
	}
	if idxBadName >= idxGood || idxGood >= idxBadNet {
		t.Errorf("expected output in original argument order (bad/name, goodpkg, badnetwork):\n%s", out)
	}

	if !strings.Contains(out, `bad/name: name must be a simple filename`) {
		t.Errorf("expected a local validation error naming the arg itself:\n%s", out)
	}
	if !strings.Contains(out, "good-1.0.0.tar.gz") {
		t.Errorf("expected goodpkg's asset listing:\n%s", out)
	}
	if !strings.Contains(out, "boom from bad repo") {
		t.Errorf("expected badnetwork's fetch error surfaced:\n%s", out)
	}
}

func TestRunInfo_MultiplePackagesEachGetFullBlock(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})
	writeManifest(t, &config.Manifest{
		Repos: map[string]string{
			"pkga": "github.com/o/a",
			"pkgb": "github.com/o/b",
		},
	})

	gh := fakeAPI(t)
	gh.AddRelease("o/a", fakegh.Release{Tag: "v1.0.0", Assets: []fakegh.Asset{{Name: "a-1.0.0.tar.gz", Body: []byte("a")}}})
	gh.AddRelease("o/b", fakegh.Release{Tag: "v2.0.0", Assets: []fakegh.Asset{{Name: "b-2.0.0.tar.gz", Body: []byte("b")}}})

	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })

	if err := runInfo(cmdWithContext(), []string{"pkga", "pkgb"}); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "a-1.0.0.tar.gz") || !strings.Contains(out, "b-2.0.0.tar.gz") {
		t.Errorf("expected both packages' assets listed:\n%s", out)
	}
	if strings.Index(out, "info: pkga") > strings.Index(out, "info: pkgb") {
		t.Errorf("expected pkga before pkgb:\n%s", out)
	}
}
