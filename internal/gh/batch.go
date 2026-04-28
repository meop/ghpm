package gh

import (
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

type releaseField struct {
	LatestRelease *struct {
		TagName string `json:"tagName"`
	} `json:"latestRelease"`
	Releases *struct {
		Nodes []struct {
			TagName string `json:"tagName"`
		} `json:"nodes"`
	} `json:"releases"`
}

func BatchLatestVersions(items []BatchItem, cacheTTL string) []BatchResult {
	results := make([]BatchResult, len(items))

	for start := 0; start < len(items); start += batchSize {
		end := start + batchSize
		if end > len(items) {
			end = len(items)
		}
		batch := items[start:end]
		batchResults := executeBatch(batch, cacheTTL)
		for i, r := range batchResults {
			results[start+i] = r
		}
	}

	return results
}

func executeBatch(items []BatchItem, cacheTTL string) []BatchResult {
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

	out, err := run("gh", args...)
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
			results[i] = BatchResult{Key: item.Key, Err: fmt.Errorf("no data for %s in batch response", item.Key)}
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
		owner := pair[0]
		repo := pair[1]

		isPinned := item.Pin.Raw != ""
		fmt.Fprintf(&b, "  %s: repository(owner: %q, name: %q) {\n", aliases[i], owner, repo)
		if isPinned {
			b.WriteString("    releases(first: 20) { nodes { tagName } }\n")
		} else {
			b.WriteString("    latestRelease { tagName }\n")
		}
		b.WriteString("  }\n")
	}
	b.WriteString("}")
	return b.String()
}

func extractTag(rf releaseField, pin config.Constraint) (string, error) {
	isPinned := pin.Raw != ""

	if isPinned {
		if rf.Releases == nil {
			return "", fmt.Errorf("no releases returned for pinned package")
		}
		bestTag := ""
		for _, node := range rf.Releases.Nodes {
			if !pin.Matches(node.TagName) {
				continue
			}
			if bestTag == "" || config.CompareVersions(node.TagName, bestTag) > 0 {
				bestTag = node.TagName
			}
		}
		if bestTag == "" {
			return "", fmt.Errorf("no release found matching %q", pin.Raw)
		}
		return bestTag, nil
	}

	if rf.LatestRelease == nil {
		return "", fmt.Errorf("no latest release returned")
	}
	return rf.LatestRelease.TagName, nil
}
