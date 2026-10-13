package toolchain

import (
	"strings"
	"testing"
)

func TestGh(t *testing.T) {
	for _, tc := range []struct{ goos, goarch, want, file string }{
		{"linux", "amd64", "gh_" + GhVersion + "_linux_amd64.tar.gz", "gh"},
		{"linux", "arm64", "gh_" + GhVersion + "_linux_arm64.tar.gz", "gh"},
		{"darwin", "amd64", "gh_" + GhVersion + "_macOS_amd64.zip", "gh"},
		{"darwin", "arm64", "gh_" + GhVersion + "_macOS_arm64.zip", "gh"},
		{"windows", "amd64", "gh_" + GhVersion + "_windows_amd64.zip", "gh.exe"},
		{"windows", "arm64", "gh_" + GhVersion + "_windows_arm64.zip", "gh.exe"},
	} {
		tool, ok := Gh(tc.goos, tc.goarch)
		if !ok || !strings.HasSuffix(tool.Asset.URL, "/v"+GhVersion+"/"+tc.want) {
			t.Errorf("Gh(%s, %s) = %q, %v; want %s", tc.goos, tc.goarch, tool.Asset.URL, ok, tc.want)
		}
		if len(tool.Asset.SHA256) != 64 || len(tool.Asset.Files) != 1 || len(tool.Asset.Files[tc.file]) != 64 {
			t.Errorf("Gh(%s, %s) does not pin its archive and its %s", tc.goos, tc.goarch, tc.file)
		}
	}
	if _, ok := Gh("freebsd", "amd64"); ok {
		t.Error("no gh is pinned for freebsd, and Gh named one")
	}
}
