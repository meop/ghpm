package cli

import (
	"testing"

	"github.com/meop/ghpm/internal/gh"
)

func names(assets []gh.Asset) []string {
	out := make([]string, len(assets))
	for i, a := range assets {
		out[i] = a.Name
	}
	return out
}

func TestResolvePriorAssets_CleanCarryOver(t *testing.T) {
	// Both stored assets resolve to a unique, distinct asset in the bumped
	// release, so the selection carries over with no prompt.
	newAssets := []gh.Asset{
		{Name: "foo-2.0-linux.tar.gz", Size: 100},
		{Name: "bar-2.0-linux.tar.gz", Size: 100},
		{Name: "foo-2.0-darwin.tar.gz", Size: 100},
	}
	old := []string{"foo-1.0-linux.tar.gz", "bar-1.0-linux.tar.gz"}
	chosens, clean := resolvePriorAssets(newAssets, old)
	if !clean {
		t.Fatalf("expected clean resolution, got clean=false")
	}
	got := names(chosens)
	if len(got) != 2 || got[0] != "foo-2.0-linux.tar.gz" || got[1] != "bar-2.0-linux.tar.gz" {
		t.Errorf("unexpected chosens: %v", got)
	}
}

func TestResolvePriorAssets_AmbiguousSplit_NotClean(t *testing.T) {
	// The release now ships two variants that the stored name matches equally
	// (the cuda-version case), so resolution is ambiguous and the whole package
	// must fall back to a fresh prompt.
	newAssets := []gh.Asset{
		{Name: "tool-cuda-12.4.tar.gz", Size: 100},
		{Name: "tool-cuda-13.3.tar.gz", Size: 100},
	}
	old := []string{"tool-cuda-13.3.tar.gz"}
	if _, clean := resolvePriorAssets(newAssets, old); clean {
		t.Errorf("expected clean=false for ambiguous match")
	}
}

func TestResolvePriorAssets_Collision_NotClean(t *testing.T) {
	// Two stored assets collapse onto the same new asset, so the count can't be
	// preserved — not clean.
	newAssets := []gh.Asset{
		{Name: "foo-2.0-linux.tar.gz", Size: 100},
	}
	old := []string{"foo-1.0-linux.tar.gz", "foo-1.0-linux.tar.gz"}
	if _, clean := resolvePriorAssets(newAssets, old); clean {
		t.Errorf("expected clean=false when two stored assets collide on one")
	}
}

func TestResolvePriorAssets_Missing_NotClean(t *testing.T) {
	// The stored asset no longer exists in the release; hint-only resolution
	// reports not-clean rather than guessing a platform asset.
	newAssets := []gh.Asset{
		{Name: "other-2.0-linux.tar.gz", Size: 100},
	}
	old := []string{"foo-1.0-linux.tar.gz"}
	if _, clean := resolvePriorAssets(newAssets, old); clean {
		t.Errorf("expected clean=false when stored asset is gone")
	}
}

func TestResolvePriorAssets_NoPriorAssets_NotClean(t *testing.T) {
	if _, clean := resolvePriorAssets(nil, nil); clean {
		t.Errorf("expected clean=false with no prior assets")
	}
}
