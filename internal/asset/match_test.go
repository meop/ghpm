package asset

import (
	"bytes"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/ui"
)

func testCfg() *config.Settings {
	return &config.Settings{
		NumParallel: 5,
	}
}

func TestIsSkipped(t *testing.T) {
	cases := []struct {
		name    string
		skipped bool
	}{
		{"fzf-0.56.0-linux_amd64.tar.gz", false},
		{"tool-0.1.0-darwin-arm64.tgz", false},
		{"tool-0.1.0-windows-amd64.zip", false},
		{"tool-0.1.0-linux-amd64.tar.bz2", false},
		{"tool-0.1.0-linux-amd64.tar.xz", false},
		{"ghpm", true},
		{"bun", true},
		{"fzf-0.56.0-linux_amd64.tar.gz.sha256", true},
		{"fzf-0.56.0-linux_amd64.tar.gz.sha256sum", true},
		{"fzf-0.56.0-linux_amd64.tar.gz.sig", true},
		{"fzf-0.56.0.deb", true},
		{"fzf-source.tar.gz", true},
		{"fzf-src.tar.gz", true},
		{"fzf.rpm", true},
		{"fzf.apk", true},
		{"fzf.msi", true},
		{"fzf.pkg", true},
		{"checksums.txt", true},
	}
	for _, c := range cases {
		got := isSkipped(c.name)
		if got != c.skipped {
			t.Errorf("isSkipped(%q) = %v, want %v", c.name, got, c.skipped)
		}
	}
}

func TestSelectAsset_ExactHint(t *testing.T) {
	assets := []gh.Asset{
		{Name: "fzf-0.56.0-linux_amd64.tar.gz", Size: 1000},
		{Name: "fzf-0.56.0-darwin_amd64.tar.gz", Size: 1000},
	}
	ac, err := SelectAssetAuto(assets, testCfg(), "fzf-0.56.0-linux_amd64.tar.gz", "")
	if err != nil {
		t.Fatal(err)
	}
	if ac.Chosen.Name != "fzf-0.56.0-linux_amd64.tar.gz" {
		t.Errorf("unexpected choice: %s", ac.Chosen.Name)
	}
}

func TestSelectAsset_PlatformMatch(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("platform-specific test")
	}
	assets := []gh.Asset{
		{Name: "tool-linux-amd64.tar.gz", Size: 100},
		{Name: "tool-darwin-amd64.tar.gz", Size: 100},
		{Name: "tool-windows-amd64.zip", Size: 100},
		{Name: "tool-linux-amd64.tar.gz.sha256", Size: 10},
	}
	ac, err := SelectAssetAuto(assets, testCfg(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if ac.Chosen.Name != "tool-linux-amd64.tar.gz" {
		t.Errorf("unexpected choice: %s", ac.Chosen.Name)
	}
}

func TestTokenize(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"claude-win32_x64.zip", []string{"claude", "win32_x64.zip"}},
		{"tool-unknown-linux-gnu-x86_64.tar.gz", []string{"tool", "unknown", "linux", "gnu", "x86_64.tar.gz"}},
		{"MyTool Darwin ARM64", []string{"mytool", "darwin", "arm64"}},
	}
	for _, c := range cases {
		got := Tokenize(c.input)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Tokenize(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestContainsAnyOf(t *testing.T) {
	cases := []struct {
		name     string
		prefixes []string
		want     bool
	}{
		{"tool-darwin-amd64.tar.gz", []string{"windows"}, false},
		{"tool-darwin-amd64.tar.gz", []string{"darwin", "macos"}, true},
		{"tool-windows-x64.zip", []string{"windows"}, true},
		{"tool-linux-amd64.tar.gz", []string{"linux"}, true},
		{"golangci-lint-1.0-darwin-amd64.tar.gz", []string{"linux"}, false},
		{"tool-macos-arm64.tar.gz", []string{"darwin", "macos"}, true},
		{"tool-osx-arm64.tar.gz", []string{"darwin", "macos", "osx"}, true},
		{"claude-win32_x64.zip", []string{"windows"}, false},
		{"tool-unknown-linux-gnu-x86_64.tar.gz", []string{"x86_64", "x64", "amd64"}, true},
		{"tool-linux-aarch64.tar.gz", []string{"arm64", "aarch64"}, true},
		{"bottom_x86_64-pc-windows-msvc.zip", []string{"x86_64", "x64", "amd64"}, true},
		{"bottom_i686-pc-windows-msvc.zip", []string{"x86_64", "x64", "amd64"}, false},
	}
	for _, c := range cases {
		got := containsAnyOf(strings.ToLower(c.name), c.prefixes)
		if got != c.want {
			t.Errorf("containsAnyOf(%q, %v) = %v, want %v", c.name, c.prefixes, got, c.want)
		}
	}
}

func TestIsVersionToken(t *testing.T) {
	cases := []struct {
		token string
		want  bool
	}{
		{"0.1.6", true},
		{"v1.2.3", true},
		{"V2.0.0", true},
		{"0.56.0", true},
		{"14", true},
		{"v14", true},
		{"bun", false},
		{"linux", false},
		{"amd64.tar.gz", false},
		{"x64.zip", false},
		{"darwin", false},
		{"gnu", false},
		{"tar.gz", false},
		{"sha256", false},
		{"win32", false},
	}
	for _, c := range cases {
		got := isVersionToken(c.token)
		if got != c.want {
			t.Errorf("isVersionToken(%q) = %v, want %v", c.token, got, c.want)
		}
	}
}

func TestStripVersionTokens(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"ghpm-0.1.6-darwin-amd64.tar.gz", []string{"ghpm", "darwin", "amd64.tar.gz"}},
		{"ghpm-0.1.7-darwin-amd64.tar.gz", []string{"ghpm", "darwin", "amd64.tar.gz"}},
		{"fzf-0.56.0-linux_amd64.tar.gz", []string{"fzf", "linux_amd64.tar.gz"}},
		{"fzf-0.71.0-linux_amd64.tar.gz", []string{"fzf", "linux_amd64.tar.gz"}},
		{"bun-v1.3.13-linux-x64.zip", []string{"bun", "linux", "x64.zip"}},
		{"bun-v1.3.14-linux-x64.zip", []string{"bun", "linux", "x64.zip"}},
		{"ghpm-0.1.7-darwin-amd64-something.tar.gz", []string{"ghpm", "darwin", "amd64", "something.tar.gz"}},
	}
	for _, c := range cases {
		tokens := Tokenize(c.input)
		got := stripVersionTokens(tokens)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("stripVersionTokens(Tokenize(%q)) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestMatchByHint_SameVersion(t *testing.T) {
	candidates := []gh.Asset{
		{Name: "ghpm-0.1.7-darwin-amd64.tar.gz", Size: 100},
		{Name: "ghpm-0.1.7-linux-amd64.tar.gz", Size: 100},
		{Name: "ghpm-0.1.7-windows-amd64.zip", Size: 100},
	}
	chosen, ok := matchByHint(candidates, "ghpm-0.1.6-darwin-amd64.tar.gz")
	if !ok {
		t.Fatal("expected match")
	}
	if chosen.Name != "ghpm-0.1.7-darwin-amd64.tar.gz" {
		t.Errorf("got %q, want ghpm-0.1.7-darwin-amd64.tar.gz", chosen.Name)
	}
}

func TestMatchByHint_CrossVersion(t *testing.T) {
	candidates := []gh.Asset{
		{Name: "fzf-0.71.0-linux_amd64.tar.gz", Size: 100},
		{Name: "fzf-0.71.0-darwin_amd64.tar.gz", Size: 100},
	}
	chosen, ok := matchByHint(candidates, "fzf-0.56.0-linux_amd64.tar.gz")
	if !ok {
		t.Fatal("expected match")
	}
	if chosen.Name != "fzf-0.71.0-linux_amd64.tar.gz" {
		t.Errorf("got %q, want fzf-0.71.0-linux_amd64.tar.gz", chosen.Name)
	}
}

func TestMatchByHint_MultipleMatches(t *testing.T) {
	candidates := []gh.Asset{
		{Name: "tool-0.2.0-linux-amd64.tar.gz", Size: 100},
		{Name: "tool-v0.2.0-linux-amd64.tar.gz", Size: 100},
	}
	_, ok := matchByHint(candidates, "tool-0.1.0-linux-amd64.tar.gz")
	if ok {
		t.Error("expected no unique match when two candidates produce same stripped tokens")
	}
}

func TestMatchByHint_DifferentStructure(t *testing.T) {
	candidates := []gh.Asset{
		{Name: "ghpm-0.1.7-darwin-amd64-something.tar.gz", Size: 100},
	}
	_, ok := matchByHint(candidates, "ghpm-0.1.6-darwin-amd64.tar.gz")
	if ok {
		t.Error("expected no match when structure differs")
	}
}

func TestMatchByHint_BunVPrefix(t *testing.T) {
	candidates := []gh.Asset{
		{Name: "bun-v1.3.14-linux-x64.zip", Size: 100},
		{Name: "bun-v1.3.14-darwin-x64.zip", Size: 100},
	}
	chosen, ok := matchByHint(candidates, "bun-v1.3.13-linux-x64.zip")
	if !ok {
		t.Fatal("expected match")
	}
	if chosen.Name != "bun-v1.3.14-linux-x64.zip" {
		t.Errorf("got %q, want bun-v1.3.14-linux-x64.zip", chosen.Name)
	}
}

func TestScoreAsset_HasNegative(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("platform-specific test")
	}
	cases := []struct {
		name    string
		wantNeg bool
	}{
		{"tool-linux-amd64.tar.gz", false},
		{"tool-darwin-amd64.tar.gz", true},
		{"tool-osx-amd64.tar.gz", true},
		{"tool-windows-amd64.zip", true},
		{"tool-linux-arm64.tar.gz", true},
		{"tool-generic.tar.gz", false},
	}
	for _, c := range cases {
		got := scoreAsset(c.name, "").hasNeg
		if got != c.wantNeg {
			t.Errorf("scoreAsset(%q) hasNeg = %v, want %v", c.name, got, c.wantNeg)
		}
	}
}

func TestSelectAssetAuto_SingleCompatible(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("platform-specific test")
	}
	assets := []gh.Asset{
		{Name: "tool-linux-amd64.tar.gz", Size: 100},
		{Name: "tool-darwin-amd64.tar.gz", Size: 100},
		{Name: "tool-windows-amd64.zip", Size: 100},
	}
	ac, err := SelectAssetAuto(assets, testCfg(), "", "tool")
	if err != nil {
		t.Fatal(err)
	}
	if ac.Chosen.Name != "tool-linux-amd64.tar.gz" {
		t.Errorf("expected auto-select, got Chosen=%q Compatible=%v", ac.Chosen.Name, ac.Compatible)
	}
}

func TestSelectAssetAuto_MultipleCompatible(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("platform-specific test")
	}
	assets := []gh.Asset{
		{Name: "tool-linux-x64.tar.gz", Size: 100},
		{Name: "tool-linux-amd64.tar.gz", Size: 100},
		{Name: "tool-darwin-amd64.tar.gz", Size: 100},
	}
	ac, err := SelectAssetAuto(assets, testCfg(), "", "tool")
	if err != nil {
		t.Fatal(err)
	}
	if ac.Chosen.Name != "" {
		t.Errorf("expected no auto-select, got %q", ac.Chosen.Name)
	}
	if len(ac.Compatible) != 2 {
		t.Errorf("expected 2 compatible, got %d: %v", len(ac.Compatible), ac.Compatible)
	}
	if len(ac.Hidden) != 1 {
		t.Errorf("expected 1 hidden, got %d: %v", len(ac.Hidden), ac.Hidden)
	}
}

func TestSelectAssetAuto_NoCompatible(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("platform-specific test")
	}
	assets := []gh.Asset{
		{Name: "tool-darwin-amd64.tar.gz", Size: 100},
		{Name: "tool-windows-amd64.zip", Size: 100},
	}
	ac, err := SelectAssetAuto(assets, testCfg(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if ac.Chosen.Name != "" {
		t.Errorf("expected no auto-select, got %q", ac.Chosen.Name)
	}
	if len(ac.Compatible) != 0 {
		t.Errorf("expected 0 compatible, got %d", len(ac.Compatible))
	}
	if len(ac.Hidden) != 2 {
		t.Errorf("expected 2 hidden, got %d", len(ac.Hidden))
	}
}

func stdinPipe(t *testing.T, input string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	ui.SetInput(r)
	t.Cleanup(func() {
		os.Stdin = oldStdin
		ui.SetInput(oldStdin)
	})
	if _, err = w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPromptWithShowMore_Empty_SelectsFirst(t *testing.T) {
	assets := []gh.Asset{
		{Name: "a.tar.gz", Size: 1},
		{Name: "b.tar.gz", Size: 2},
	}
	stdinPipe(t, "\n")
	got, err := promptWithShowMore(assets, nil, "")
	if err != nil || got.Name != "a.tar.gz" {
		t.Errorf("got %q, %v; want a.tar.gz, nil", got.Name, err)
	}
}

func TestPromptWithShowMore_SelectSecond(t *testing.T) {
	assets := []gh.Asset{
		{Name: "a.tar.gz", Size: 1},
		{Name: "b.tar.gz", Size: 2},
	}
	stdinPipe(t, "2\n")
	got, err := promptWithShowMore(assets, nil, "")
	if err != nil || got.Name != "b.tar.gz" {
		t.Errorf("got %q, %v; want b.tar.gz, nil", got.Name, err)
	}
}

func TestPromptWithShowMore_Skip(t *testing.T) {
	assets := []gh.Asset{
		{Name: "tool-linux-amd64.tar.gz", Size: 100},
		{Name: "tool-darwin-amd64.tar.gz", Size: 100},
	}
	stdinPipe(t, "0\n")
	_, err := promptWithShowMore(assets, nil, "")
	if err != ErrSkip {
		t.Errorf("expected ErrSkip, got %v", err)
	}
}

func TestPromptWithShowMore_ShowMore_Empty_SelectsFirst(t *testing.T) {
	compatible := []gh.Asset{{Name: "a.tar.gz", Size: 1}, {Name: "b.tar.gz", Size: 2}}
	hidden := []gh.Asset{{Name: "c.tar.gz", Size: 3}}
	stdinPipe(t, "3\n\n")
	got, err := promptWithShowMore(compatible, hidden, "")
	if err != nil || got.Name != "a.tar.gz" {
		t.Errorf("got %q, %v; want a.tar.gz, nil", got.Name, err)
	}
}

// TestPromptFromCandidates_Label guards that the single-asset prompt names its
// package when it has no preceding context line (sync/download/upgrade).
func TestPromptFromCandidates_Label(t *testing.T) {
	stdinPipe(t, "\n")
	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })
	ac := AssetCandidates{Compatible: []gh.Asset{
		{Name: "a.tar.gz", Size: 1},
		{Name: "b.tar.gz", Size: 2},
	}}
	got, err := PromptFromCandidates(ac, "yay")
	if err != nil || got.Name != "a.tar.gz" {
		t.Fatalf("got %q, %v; want a.tar.gz, nil", got.Name, err)
	}
	if !strings.Contains(buf.String(), "yay: choose asset:\n") {
		t.Errorf("missing package label in prompt header:\n%q", buf.String())
	}
}

func TestPromptSelect_Empty_SelectsFirst(t *testing.T) {
	assets := []gh.Asset{
		{Name: "a.tar.gz", Size: 1},
		{Name: "b.tar.gz", Size: 2},
	}
	stdinPipe(t, "\n")
	got, err := PromptSelect("choose:", assets, "")
	if err != nil || got.Name != "a.tar.gz" {
		t.Errorf("got %q, %v; want a.tar.gz, nil", got.Name, err)
	}
}

func TestPromptSelect_SelectSecond(t *testing.T) {
	assets := []gh.Asset{
		{Name: "a.tar.gz", Size: 1},
		{Name: "b.tar.gz", Size: 2},
	}
	stdinPipe(t, "2\n")
	got, err := PromptSelect("choose:", assets, "")
	if err != nil || got.Name != "b.tar.gz" {
		t.Errorf("got %q, %v; want b.tar.gz, nil", got.Name, err)
	}
}

func TestPromptSelect_Skip(t *testing.T) {
	assets := []gh.Asset{
		{Name: "tool-linux-amd64.tar.gz", Size: 100},
		{Name: "tool-darwin-amd64.tar.gz", Size: 100},
	}
	stdinPipe(t, "0\n")
	_, err := PromptSelect("choose:", assets, "")
	if err != ErrSkip {
		t.Errorf("expected ErrSkip, got %v", err)
	}
}

func TestPromptAssetsMulti_AutoChosen(t *testing.T) {
	chosen := gh.Asset{Name: "a.tar.gz", Size: 1}
	got, err := PromptAssetsMulti(AssetCandidates{Chosen: chosen}, "")
	if err != nil || len(got) != 1 || got[0].Name != "a.tar.gz" {
		t.Errorf("got %v, %v; want [a.tar.gz], nil", got, err)
	}
}

func TestPromptAssetsMulti_Empty_SelectsFirst(t *testing.T) {
	compatible := []gh.Asset{
		{Name: "a.tar.gz", Size: 1},
		{Name: "b.tar.gz", Size: 2},
	}
	stdinPipe(t, "\n")
	got, err := PromptAssetsMulti(AssetCandidates{Compatible: compatible}, "")
	if err != nil || len(got) != 1 || got[0].Name != "a.tar.gz" {
		t.Errorf("got %v, %v; want [a.tar.gz], nil", got, err)
	}
}

func TestPromptAssetsMulti_SelectMultiple(t *testing.T) {
	compatible := []gh.Asset{
		{Name: "a.tar.gz", Size: 1},
		{Name: "b.tar.gz", Size: 2},
	}
	stdinPipe(t, "1,2\n")
	got, err := PromptAssetsMulti(AssetCandidates{Compatible: compatible}, "")
	if err != nil || len(got) != 2 {
		t.Errorf("got %v, %v; want 2 assets, nil", got, err)
	}
}

func TestPromptAssetsMulti_Skip(t *testing.T) {
	compatible := []gh.Asset{
		{Name: "a.tar.gz", Size: 1},
		{Name: "b.tar.gz", Size: 2},
	}
	stdinPipe(t, "0\n")
	_, err := PromptAssetsMulti(AssetCandidates{Compatible: compatible}, "")
	if err != ErrSkip {
		t.Errorf("expected ErrSkip, got %v", err)
	}
}

func TestPromptAssetsMulti_ShowMore_Empty_SelectsFirst(t *testing.T) {
	compatible := []gh.Asset{{Name: "a.tar.gz", Size: 1}, {Name: "b.tar.gz", Size: 2}}
	hidden := []gh.Asset{{Name: "c.tar.gz", Size: 3}}
	stdinPipe(t, "3\n\n")
	got, err := PromptAssetsMulti(AssetCandidates{Compatible: compatible, Hidden: hidden}, "")
	if err != nil || len(got) != 1 || got[0].Name != "a.tar.gz" {
		t.Errorf("got %v, %v; want [a.tar.gz], nil", got, err)
	}
}

func TestTokensMatch(t *testing.T) {
	cases := []struct {
		a, b []string
		want bool
	}{
		{[]string{"a", "b"}, []string{"a", "b"}, true},
		{[]string{"a", "b"}, []string{"a", "c"}, false},
		{[]string{"a"}, []string{"a", "b"}, false},
		{[]string{}, []string{}, true},
		{nil, nil, true},
		{[]string{"a"}, nil, false},
	}
	for _, c := range cases {
		got := tokensMatch(c.a, c.b)
		if got != c.want {
			t.Errorf("tokensMatch(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestStripAssetExt(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"tool-linux-amd64.tar.gz", "tool-linux-amd64"},
		{"tool-linux-amd64.tgz", "tool-linux-amd64"},
		{"tool-linux-amd64.tar.bz2", "tool-linux-amd64"},
		{"tool-linux-amd64.tar.xz", "tool-linux-amd64"},
		{"tool-linux-amd64.zip", "tool-linux-amd64"},
		{"tool-linux-amd64", "tool-linux-amd64"},
		{"Tool-Linux-AMD64.TAR.GZ", "tool-linux-amd64"},
	}
	for _, c := range cases {
		if got := stripAssetExt(c.name); got != c.want {
			t.Errorf("stripAssetExt(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSelectAssetAuto_Dedup(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("platform-specific test")
	}
	assets := []gh.Asset{
		{Name: "tool-linux-amd64.tar.gz", Size: 100},
		{Name: "tool-linux-amd64.zip", Size: 100},
		{Name: "tool-darwin-amd64.tar.gz", Size: 100},
	}
	ac, err := SelectAssetAuto(assets, testCfg(), "", "tool")
	if err != nil {
		t.Fatal(err)
	}
	if ac.Chosen.Name != "tool-linux-amd64.tar.gz" {
		t.Errorf("expected auto-select of tar.gz, got Chosen=%q Compatible=%v", ac.Chosen.Name, ac.Compatible)
	}
}

func TestSelectAssetAuto_CompatibleAlphabeticalOrder(t *testing.T) {
	assets := []gh.Asset{
		{Name: "tool-zzz.zip", Size: 100},
		{Name: "tool-aaa.zip", Size: 100},
		{Name: "tool-mmm.zip", Size: 100},
	}
	ac, err := SelectAssetAuto(assets, testCfg(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ac.Compatible) != 3 {
		t.Fatalf("expected 3 compatible, got %d", len(ac.Compatible))
	}
	want := []string{"tool-aaa.zip", "tool-mmm.zip", "tool-zzz.zip"}
	for i, a := range ac.Compatible {
		if a.Name != want[i] {
			t.Errorf("Compatible[%d] = %q, want %q", i, a.Name, want[i])
		}
	}
}

func TestSecondaryScore_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-specific test")
	}
	cases := []struct {
		name string
		want int
	}{
		{"tool-linux-gnu-amd64.tar.gz", 7},  // gnu(+2) + tar.gz(+5)
		{"tool-linux-musl-amd64.tar.gz", 6}, // musl(+1) + tar.gz(+5)
		{"tool-linux-amd64.tar.gz", 5},      // tar.gz(+5)
		{"tool-linux-amd64.tgz", 4},         // tgz(+4)
		{"tool-linux-amd64.tar.bz2", 3},     // tar.bz2(+3)
		{"tool-linux-amd64.tar.xz", 2},      // tar.xz(+2)
		{"tool-linux-gnu-amd64.zip", 3},     // gnu(+2) + zip(+1)
		{"tool-linux-amd64.zip", 1},         // zip(+1)
	}
	for _, c := range cases {
		if got := secondaryScore(c.name); got != c.want {
			t.Errorf("secondaryScore(%q) = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestSecondaryScore_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific test")
	}
	cases := []struct {
		name string
		want int
	}{
		{"bottom_x86_64-pc-windows-msvc.zip", 7}, // msvc(+2) + zip(+5)
		{"bottom_x86_64-pc-windows-gnu.zip", 6},  // gnu(+1) + zip(+5)
		{"bottom_i686-pc-windows-msvc.zip", 7},   // msvc(+2) + zip(+5)
		{"tool-windows-msvc.tar.gz", 6},          // msvc(+2) + tar.gz(+4)
		{"tool-windows-gnu.tar.gz", 5},           // gnu(+1) + tar.gz(+4)
		{"tool-windows-msvc.tar.xz", 3},          // msvc(+2) + tar.xz(+1)
		{"tool-windows-amd64.zip", 5},            // zip(+5)
	}
	for _, c := range cases {
		if got := secondaryScore(c.name); got != c.want {
			t.Errorf("secondaryScore(%q) = %d, want %d", c.name, got, c.want)
		}
	}
}
