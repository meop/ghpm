package version

import "testing"

func TestNormalize(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"bun-v1.3.13", "1.3.13"},
		{"v0.71.0", "0.71.0"},
		{"1.2.3", "1.2.3"},
		{"", ""},
		{"abc", "abc"},
		{"v1", "1"},
	}
	for _, c := range cases {
		got := Normalize(c.input)
		if got != c.want {
			t.Errorf("Normalize(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestSplitJunk(t *testing.T) {
	cases := []struct {
		input                              string
		wantLeading, wantVer, wantTrailing string
		wantOk                             bool
	}{
		{"b1234", "b", "1234", "", true},
		{"llama-b1234-bin-ubuntu-x64.zip", "llama-b", "1234", "-bin-ubuntu-x64.zip", true},
		{"v1.2.3.4", "v", "1.2.3.4", "", true},
		{"bun-v1.2.3.4", "bun-v", "1.2.3.4", "", true},
		{"bun-v1.2.3.4-bun.tar.gz", "bun-v", "1.2.3.4", "-bun.tar.gz", true},
		{"1.2.3", "", "1.2.3", "", true},
		// The version doesn't swallow a trailing "." that belongs to the
		// following extension, not the version itself.
		{"v1.2.3.zip", "v", "1.2.3", ".zip", true},
		{"abc", "", "", "", false},
		{"", "", "", "", false},
	}
	for _, c := range cases {
		leading, ver, trailing, ok := SplitJunk(c.input)
		if leading != c.wantLeading || ver != c.wantVer || trailing != c.wantTrailing || ok != c.wantOk {
			t.Errorf("SplitJunk(%q) = (%q, %q, %q, %v), want (%q, %q, %q, %v)",
				c.input, leading, ver, trailing, ok, c.wantLeading, c.wantVer, c.wantTrailing, c.wantOk)
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
		got := IsVersionToken(c.token)
		if got != c.want {
			t.Errorf("IsVersionToken(%q) = %v, want %v", c.token, got, c.want)
		}
	}
}
