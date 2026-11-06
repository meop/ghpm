package toolchain

import (
	"slices"
	"testing"
)

func TestGh(t *testing.T) {
	for _, tc := range []struct{ goos, goarch, want, file string }{
		{"linux", "amd64", "gh_2.103.3_linux_amd64.tar.gz", "gh"},
		{"linux", "arm64", "gh_2.103.3_linux_arm64.tar.gz", "gh"},
		{"darwin", "arm64", "gh_2.103.3_macOS_arm64.zip", "gh"},
		{"windows", "amd64", "gh_2.103.3_windows_amd64.zip", "gh.exe"},
		{"windows", "arm64", "gh_2.103.3_windows_arm64.zip", "gh.exe"},
	} {
		r, ok := Gh(tc.goos, tc.goarch)
		if !ok {
			t.Errorf("Gh(%s, %s) names nothing", tc.goos, tc.goarch)
			continue
		}
		if got, _ := r.Archive("2.103.3"); got != tc.want {
			t.Errorf("Gh(%s, %s) archive = %s, want %s", tc.goos, tc.goarch, got, tc.want)
		}
		if got := r.Sums("2.103.3"); got != "gh_2.103.3_checksums.txt" {
			t.Errorf("Gh(%s, %s) checksums = %s", tc.goos, tc.goarch, got)
		}
		if !slices.Equal(r.Files, []string{tc.file}) {
			t.Errorf("Gh(%s, %s) keeps %v, want %s", tc.goos, tc.goarch, r.Files, tc.file)
		}
	}
	for _, p := range [][2]string{{"freebsd", "amd64"}, {"linux", "386"}} {
		if _, ok := Gh(p[0], p[1]); ok {
			t.Errorf("gh is not vendored for %s/%s, and Gh named one", p[0], p[1])
		}
	}
}
