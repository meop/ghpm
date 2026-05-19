package gh

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/meop/ghpm/internal/config"
)

func TestBuildBatchQuery_Floating(t *testing.T) {
	items := []BatchItem{
		{Key: "fzf", Source: "github.com/junegunn/fzf"},
		{Key: "gh", Source: "github.com/cli/cli"},
	}
	aliases := []string{"r0", "r1"}
	ownerMap := map[string][2]string{
		"r0": {"junegunn", "fzf"},
		"r1": {"cli", "cli"},
	}

	query := buildBatchQuery(items, aliases, ownerMap)

	if !strings.Contains(query, "latestRelease") {
		t.Error("expected latestRelease for floating packages")
	}
	if strings.Contains(query, "refs(") {
		t.Error("should not request refs for floating packages")
	}
	if !strings.Contains(query, `owner: "junegunn"`) {
		t.Error("expected junegunn owner in query")
	}
	if !strings.Contains(query, `name: "cli"`) {
		t.Error("expected cli repo name in query")
	}
}

func TestBuildBatchQuery_Pinned(t *testing.T) {
	c, _ := config.ParseConstraint("14")
	items := []BatchItem{
		{Key: "rg@14", Source: "github.com/BurntSushi/ripgrep", Pin: c},
	}
	aliases := []string{"r0"}
	ownerMap := map[string][2]string{
		"r0": {"BurntSushi", "ripgrep"},
	}

	query := buildBatchQuery(items, aliases, ownerMap)

	if !strings.Contains(query, `refPrefix: "refs/tags/v14."`) {
		t.Error("expected v-prefixed refs query for pinned major")
	}
	if !strings.Contains(query, `refPrefix: "refs/tags/14."`) {
		t.Error("expected non-v refs query for pinned major")
	}
	if strings.Contains(query, "latestRelease") {
		t.Error("should not request latestRelease for pinned packages")
	}
}

func TestBuildBatchQuery_PinnedMinor(t *testing.T) {
	c, _ := config.ParseConstraint("0.70")
	items := []BatchItem{
		{Key: "fzf@0.70", Source: "github.com/junegunn/fzf", Pin: c},
	}
	aliases := []string{"r0"}
	ownerMap := map[string][2]string{
		"r0": {"junegunn", "fzf"},
	}

	query := buildBatchQuery(items, aliases, ownerMap)

	if !strings.Contains(query, `refPrefix: "refs/tags/v0.70."`) {
		t.Error("expected v-prefixed refs query for pinned minor")
	}
	if !strings.Contains(query, `refPrefix: "refs/tags/0.70."`) {
		t.Error("expected non-v refs query for pinned minor")
	}
}

func TestBuildBatchQuery_Mixed(t *testing.T) {
	c, _ := config.ParseConstraint("0.70")
	items := []BatchItem{
		{Key: "fzf", Source: "github.com/junegunn/fzf"},
		{Key: "fzf@0.70", Source: "github.com/junegunn/fzf", Pin: c},
	}
	aliases := []string{"r0", "r1"}
	ownerMap := map[string][2]string{
		"r0": {"junegunn", "fzf"},
		"r1": {"junegunn", "fzf"},
	}

	query := buildBatchQuery(items, aliases, ownerMap)

	if !strings.Contains(query, "r0: repository") {
		t.Error("expected r0 alias")
	}
	if !strings.Contains(query, "r1: repository") {
		t.Error("expected r1 alias")
	}
}

func TestExtractTag_Floating(t *testing.T) {
	raw := `{"latestRelease":{"tagName":"v1.2.3"}}`
	var rf releaseField
	if err := json.Unmarshal([]byte(raw), &rf); err != nil {
		t.Fatal(err)
	}
	tag, err := extractTag(rf, config.Constraint{})
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v1.2.3" {
		t.Errorf("got %q, want v1.2.3", tag)
	}
}

func TestExtractTag_Pinned(t *testing.T) {
	raw := `{"vRefs":{"nodes":[{"name":"v1.3.0"},{"name":"v1.2.0"},{"name":"v1.1.0"}]}}`
	var rf releaseField
	if err := json.Unmarshal([]byte(raw), &rf); err != nil {
		t.Fatal(err)
	}
	c, _ := config.ParseConstraint("1")
	tag, err := extractTag(rf, c)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v1.3.0" {
		t.Errorf("got %q, want v1.3.0 (highest matching v1.x)", tag)
	}
}

func TestExtractTag_PinnedSkipsPrerelease(t *testing.T) {
	raw := `{"vRefs":{"nodes":[{"name":"v1.4.0-rc.1"},{"name":"v1.3.0"},{"name":"v1.2.0"}]}}`
	var rf releaseField
	if err := json.Unmarshal([]byte(raw), &rf); err != nil {
		t.Fatal(err)
	}
	c, _ := config.ParseConstraint("1")
	tag, err := extractTag(rf, c)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v1.3.0" {
		t.Errorf("got %q, want v1.3.0 (skipping rc pre-release)", tag)
	}
}

func TestExtractTag_PinnedMergesVAndNonV(t *testing.T) {
	raw := `{"nvRefs":{"nodes":[{"name":"1.3.0"},{"name":"1.2.0"}]}}`
	var rf releaseField
	if err := json.Unmarshal([]byte(raw), &rf); err != nil {
		t.Fatal(err)
	}
	c, _ := config.ParseConstraint("1")
	tag, err := extractTag(rf, c)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "1.3.0" {
		t.Errorf("got %q, want 1.3.0 (non-v prefix repo)", tag)
	}
}

func TestExtractTag_PinnedNoMatch(t *testing.T) {
	raw := `{"vRefs":{"nodes":[{"name":"v1.0.0"}]}}`
	var rf releaseField
	if err := json.Unmarshal([]byte(raw), &rf); err != nil {
		t.Fatal(err)
	}
	c, _ := config.ParseConstraint("99")
	_, err := extractTag(rf, c)
	if err == nil {
		t.Error("expected error when no matching release")
	}
}

func TestExtractTag_NoLatestRelease(t *testing.T) {
	raw := `{}`
	var rf releaseField
	if err := json.Unmarshal([]byte(raw), &rf); err != nil {
		t.Fatal(err)
	}
	_, err := extractTag(rf, config.Constraint{})
	if err == nil {
		t.Error("expected error when no latest release")
	}
}
