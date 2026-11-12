package gh

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/meop/ghpm/internal/config"
)

const batchSize = 50

type BatchItem struct {
	Key    string
	Source string
	Pin    config.Constraint
}

type BatchResult struct {
	Key       string
	LatestTag string
	Err       error
}

type graphQLResponse struct {
	Data   map[string]json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// errorMessages joins the response's top-level GraphQL errors, if any. `gh api
// graphql` exits 0 for a request that reached the server successfully even
// when individual aliased queries in the batch failed (e.g. a renamed/deleted
// repo, a scope issue, or a rate-limit message from the API itself) — those
// failures show up here, not as a runCmd error, and are otherwise silently
// available-but-unread on the parsed response.
func (resp graphQLResponse) errorMessages() string {
	msgs := make([]string, 0, len(resp.Errors))
	for _, e := range resp.Errors {
		if e.Message != "" {
			msgs = append(msgs, e.Message)
		}
	}
	return strings.Join(msgs, "; ")
}

type releaseField struct {
	LatestRelease *struct {
		TagName string `json:"tagName"`
	} `json:"latestRelease"`
	VRefs *struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"vRefs"`
	NvRefs *struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"nvRefs"`
}

func BatchLatestVersions(ctx context.Context, items []BatchItem, cacheTTL string) []BatchResult {
	results := make([]BatchResult, len(items))

	for start := 0; start < len(items); start += batchSize {
		end := min(start+batchSize, len(items))
		batchResults := executeBatch(ctx, items[start:end], cacheTTL)
		for i, r := range batchResults {
			results[start+i] = r
		}
	}

	return results
}

func executeBatch(ctx context.Context, items []BatchItem, cacheTTL string) []BatchResult {
	results := make([]BatchResult, len(items))

	aliases := make([]string, len(items))
	ownerMap := make(map[string][2]string)

	for i, item := range items {
		owner, repo, err := SplitSource(item.Source)
		if err != nil {
			results[i] = BatchResult{Key: item.Key, Err: err}
			continue
		}
		alias := fmt.Sprintf("r%d", i)
		aliases[i] = alias
		ownerMap[alias] = [2]string{owner, repo}
	}

	query := buildBatchQuery(items, aliases, ownerMap)

	args := []string{"api", "graphql", "-f", "query=" + query}
	if cacheTTL != "" {
		args = append(args, "--cache", cacheTTL)
	}

	out, err := runCmd(ctx, "gh", args...)
	if err != nil {
		for i := range items {
			if results[i].Err != nil {
				continue
			}
			results[i] = BatchResult{Key: items[i].Key, Err: err}
		}
		return results
	}

	var resp graphQLResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		for i := range items {
			if results[i].Err != nil {
				continue
			}
			results[i] = BatchResult{Key: items[i].Key, Err: fmt.Errorf("parsing batch response: %w", err)}
		}
		return results
	}

	for i, item := range items {
		if results[i].Err != nil {
			continue
		}
		alias := aliases[i]
		raw, ok := resp.Data[alias]
		if !ok {
			if msg := resp.errorMessages(); msg != "" {
				results[i] = BatchResult{Key: item.Key, Err: fmt.Errorf("%s", msg)}
			} else {
				results[i] = BatchResult{Key: item.Key, Err: fmt.Errorf("no data for %s in batch response", item.Key)}
			}
			continue
		}
		var rf releaseField
		if err := json.Unmarshal(raw, &rf); err != nil {
			results[i] = BatchResult{Key: item.Key, Err: fmt.Errorf("parsing release data for %s: %w", item.Key, err)}
			continue
		}

		tag, err := extractTag(rf, item.Pin)
		if err != nil {
			results[i] = BatchResult{Key: item.Key, Err: err}
			continue
		}
		results[i] = BatchResult{Key: item.Key, LatestTag: tag}
	}

	return results
}

func buildBatchQuery(items []BatchItem, aliases []string, ownerMap map[string][2]string) string {
	var b strings.Builder
	b.WriteString("{\n")
	for i, item := range items {
		if aliases[i] == "" {
			continue
		}
		pair := ownerMap[aliases[i]]
		fmt.Fprintf(&b, "  %s: repository(owner: %q, name: %q) {\n", aliases[i], pair[0], pair[1])
		if item.Pin.Raw != "" {
			vq, nvq := tagQueries(item.Pin)
			b.WriteString("    latestRelease { tagName }\n")
			fmt.Fprintf(&b, "    vRefs: refs(refPrefix: \"refs/tags/\", query: %q, orderBy: {field: TAG_COMMIT_DATE, direction: DESC}, first: 100) { nodes { name } }\n", vq)
			fmt.Fprintf(&b, "    nvRefs: refs(refPrefix: \"refs/tags/\", query: %q, orderBy: {field: TAG_COMMIT_DATE, direction: DESC}, first: 100) { nodes { name } }\n", nvq)
		} else {
			b.WriteString("    latestRelease { tagName }\n")
		}
		b.WriteString("  }\n")
	}
	b.WriteString("}")
	return b.String()
}

// tagQueries are the tag name filters for a constraint, v-prefixed and not. GitHub takes a refPrefix only as a whole
// ref namespace ("refs/tags/"), so the version goes in the name query, and extractTag keeps what the pin matches.
func tagQueries(c config.Constraint) (vQuery, nvQuery string) {
	var base string
	if c.Level == config.PinMinor {
		base = fmt.Sprintf("%d.%d.", c.Major, c.Minor)
	} else {
		base = fmt.Sprintf("%d.", c.Major)
	}
	return "v" + base, base
}

func extractTag(rf releaseField, pin config.Constraint) (string, error) {
	if pin.Raw == "" {
		if rf.LatestRelease == nil {
			return "", fmt.Errorf("no latest release returned")
		}
		return rf.LatestRelease.TagName, nil
	}

	var candidates []string
	if rf.VRefs != nil {
		for _, n := range rf.VRefs.Nodes {
			candidates = append(candidates, n.Name)
		}
	}
	if rf.NvRefs != nil {
		for _, n := range rf.NvRefs.Nodes {
			candidates = append(candidates, n.Name)
		}
	}
	best := func(keep func(string) bool) string {
		bestTag := ""
		for _, tag := range candidates {
			if isPrereleaseName(tag) || !pin.Matches(tag) || !keep(tag) {
				continue
			}
			if bestTag == "" || config.CompareVersions(tag, bestTag) > 0 {
				bestTag = tag
			}
		}
		return bestTag
	}
	// where two tags carry the version, as a fork's own beside its upstream's can, the prefix of the latest
	// release wins, as it does for an exact pin
	if rf.LatestRelease != nil {
		latest := tagPrefix(rf.LatestRelease.TagName)
		if tag := best(func(t string) bool { return tagPrefix(t) == latest }); tag != "" {
			return tag, nil
		}
	}
	if tag := best(func(string) bool { return true }); tag != "" {
		return tag, nil
	}
	return "", fmt.Errorf("no release found matching %q", pin.Raw)
}

// tagPrefix is what a tag carries before its version: "v", "bun-v", "zcode-cli-v", or nothing.
func tagPrefix(tag string) string { return strings.TrimSuffix(tag, config.NormalizeVersion(tag)) }

// isPrereleaseName reports whether a tag looks like a pre-release.
// Standard semver uses a hyphen separator for pre-release identifiers (e.g. v1.0.0-rc.1).
func isPrereleaseName(tag string) bool {
	return strings.Contains(config.NormalizeVersion(tag), "-")
}
