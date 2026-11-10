package cli

import (
	"context"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
)

// releaseForClient answers each lookup with a release tagged for the call that made it, so a test reads which lookup
// a version led to off the tag it gets back.
type releaseForClient struct {
	fakeGHClient
	matched config.Constraint
}

func (c *releaseForClient) GetLatestRelease(context.Context, string, string) (gh.Release, error) {
	return gh.Release{TagName: "latest"}, nil
}

func (c *releaseForClient) GetReleaseByTag(_ context.Context, _, _, tag string) (gh.Release, error) {
	return gh.Release{TagName: "tag " + tag}, nil
}

func (c *releaseForClient) FindLatestMatching(_ context.Context, _, _ string, con config.Constraint) (gh.Release, error) {
	c.matched = con
	return gh.Release{TagName: "matching"}, nil
}

func TestReleaseFor(t *testing.T) {
	cases := []struct {
		ver   string
		want  string
		level config.PinLevel
	}{
		{ver: "", want: "latest"},
		{ver: "2", want: "matching", level: config.PinMajor},
		{ver: "2.2", want: "matching", level: config.PinMinor},
		{ver: "v2", want: "matching", level: config.PinMajor},
		{ver: "2.2.6", want: "tag 2.2.6"},
		{ver: "v2.2.6", want: "tag v2.2.6"},
	}
	for _, tc := range cases {
		t.Run(tc.ver, func(t *testing.T) {
			client := &releaseForClient{}
			rel, err := releaseFor(context.Background(), client, "o", "r", tc.ver)
			if err != nil {
				t.Fatalf("releaseFor(%q): %v", tc.ver, err)
			}
			if rel.TagName != tc.want {
				t.Errorf("releaseFor(%q) = %q, want %q", tc.ver, rel.TagName, tc.want)
			}
			if tc.want == "matching" && client.matched.Level != tc.level {
				t.Errorf("releaseFor(%q) matched at level %v, want %v", tc.ver, client.matched.Level, tc.level)
			}
		})
	}
}

func TestReleaseFor_BadVersion(t *testing.T) {
	if _, err := releaseFor(context.Background(), &releaseForClient{}, "o", "r", "not-a-version"); err == nil {
		t.Error("expected an error for a version that is not one")
	}
}
