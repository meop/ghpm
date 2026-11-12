package gh

import (
	"context"
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

	if !strings.Contains(query, `refPrefix: "refs/tags/", query: "v14."`) {
		t.Error("expected v-prefixed refs query for pinned major")
	}
	if !strings.Contains(query, `refPrefix: "refs/tags/", query: "14."`) {
		t.Error("expected non-v refs query for pinned major")
	}
	if !strings.Contains(query, "latestRelease") {
		t.Error("expected latestRelease for pinned packages, whose tag prefix the pin prefers")
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

	if !strings.Contains(query, `refPrefix: "refs/tags/", query: "v0.70."`) {
		t.Error("expected v-prefixed refs query for pinned minor")
	}
	if !strings.Contains(query, `refPrefix: "refs/tags/", query: "0.70."`) {
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

// TestBatchLatestVersions_SurfacesGraphQLError is the regression test for a
// real gap: the response's top-level "errors" array was decoded into
// graphQLResponse.Errors but never read, so an item whose alias was missing
// from "data" (a per-query GraphQL failure — bad repo name, missing scope,
// server-side rate limit — while the request itself still answers 200) fell
// through to a generic "no data for %s" message. That not only threw away the
// real reason but could hide a rate limit from gh.IsRateLimited's substring
// check downstream, since the generic message never contains "rate limit".
// TestExecuteBatch_InvalidSourceSkipped confirms one item with a malformed
// Source doesn't take the whole batch down — it fails on its own, and other
// items are still queried and resolved normally.
func TestExecuteBatch_InvalidSourceSkipped(t *testing.T) {
	fakeGraphQL(t, 200, `{"data":{"r1":{"latestRelease":{"tagName":"v1.0.0"}}}}`)

	items := []BatchItem{
		{Key: "bad", Source: "not-a-valid-source"},
		{Key: "good", Source: "github.com/owner/repo"},
	}
	results := BatchLatestVersions(context.Background(), items, "")

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Err == nil {
		t.Error("expected error for invalid source")
	}
	if results[1].Err != nil {
		t.Errorf("expected no error for valid item, got %v", results[1].Err)
	}
	if results[1].LatestTag != "v1.0.0" {
		t.Errorf("got tag %q, want v1.0.0", results[1].LatestTag)
	}
}

// TestExecuteBatch_CommandFailure confirms a request that fails outright (a
// server error, a refused token, ...) surfaces as every item's error, not just
// a subset or a panic.
func TestExecuteBatch_CommandFailure(t *testing.T) {
	fakeGraphQL(t, 502, "boom")

	items := []BatchItem{
		{Key: "one", Source: "github.com/owner/repo1"},
		{Key: "two", Source: "github.com/owner/repo2"},
	}
	results := BatchLatestVersions(context.Background(), items, "")

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for i, r := range results {
		if r.Err == nil {
			t.Errorf("result[%d]: expected error, got nil", i)
		}
	}
}

// TestExecuteBatch_MalformedResponse confirms invalid JSON from GraphQL is
// reported per-item rather than panicking.
func TestExecuteBatch_MalformedResponse(t *testing.T) {
	fakeGraphQL(t, 200, `not json`)

	items := []BatchItem{{Key: "fzf", Source: "github.com/owner/repo"}}
	results := BatchLatestVersions(context.Background(), items, "")

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("expected a parse error")
	}
}

// TestExecuteBatch_MissingDataNoErrors covers the "no data for %s" fallback
// specifically when the response has neither the expected alias nor any
// top-level GraphQL errors to explain why (distinct from
// TestBatchLatestVersions_SurfacesGraphQLError, which has errors to surface).
func TestExecuteBatch_MissingDataNoErrors(t *testing.T) {
	fakeGraphQL(t, 200, `{"data":{}}`)

	items := []BatchItem{{Key: "fzf", Source: "github.com/owner/repo"}}
	results := BatchLatestVersions(context.Background(), items, "")

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err == nil || !strings.Contains(results[0].Err.Error(), "no data for") {
		t.Errorf("expected generic 'no data for' fallback, got: %v", results[0].Err)
	}
}

func TestBatchLatestVersions_SurfacesGraphQLError(t *testing.T) {
	fakeGraphQL(t, 200, `{"data":{},"errors":[{"message":"Could not resolve to a Repository with the name owner/repo."}]}`)

	items := []BatchItem{{Key: "fzf", Source: "github.com/owner/repo"}}
	results := BatchLatestVersions(context.Background(), items, "")

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(results[0].Err.Error(), "Could not resolve to a Repository") {
		t.Errorf("expected the real GraphQL error message surfaced, got: %v", results[0].Err)
	}
	if strings.Contains(results[0].Err.Error(), "no data for") {
		t.Errorf("expected the real error, not the generic fallback, got: %v", results[0].Err)
	}
}

func TestExtractTag_PinnedPrefersTheLatestReleasesPrefix(t *testing.T) {
	// a fork keeps its upstream's v<semver> tags beside its own releases' tags
	raw := `{"latestRelease":{"tagName":"zcode-cli-v3.17.2"},
		"vRefs":{"nodes":[{"name":"v3.18.0"},{"name":"zcode-cli-v3.17.2"},{"name":"v3.17.2"},{"name":"zcode-cli-v3.18.0-alpha.1"}]}}`
	var rf releaseField
	if err := json.Unmarshal([]byte(raw), &rf); err != nil {
		t.Fatal(err)
	}
	c, _ := config.ParseConstraint("3")
	tag, err := extractTag(rf, c)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "zcode-cli-v3.17.2" {
		t.Errorf("got %q, want zcode-cli-v3.17.2 (the latest release's stream, its newest stable)", tag)
	}
}

func TestExtractTag_PinnedFallsBackPastTheLatestReleasesPrefix(t *testing.T) {
	raw := `{"latestRelease":{"tagName":"zcode-cli-v2.0.0"},"vRefs":{"nodes":[{"name":"v3.18.0"}]}}`
	var rf releaseField
	if err := json.Unmarshal([]byte(raw), &rf); err != nil {
		t.Fatal(err)
	}
	c, _ := config.ParseConstraint("3")
	tag, err := extractTag(rf, c)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v3.18.0" {
		t.Errorf("got %q, want v3.18.0 when no tag of the latest release's stream matches the pin", tag)
	}
}

func TestExtractTag_PinnedKeepsAPrefixedLatestStream(t *testing.T) {
	raw := `{"latestRelease":{"tagName":"bun-v1.4.4"},"vRefs":{"nodes":[{"name":"bun-v1.3.13"},{"name":"bun-v1.3.2"}]}}`
	var rf releaseField
	if err := json.Unmarshal([]byte(raw), &rf); err != nil {
		t.Fatal(err)
	}
	c, _ := config.ParseConstraint("1.3")
	tag, err := extractTag(rf, c)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "bun-v1.3.13" {
		t.Errorf("got %q, want bun-v1.3.13", tag)
	}
}
