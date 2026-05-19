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
	VRefs *struct {
		Nodes []struct{ Name string `json:"name"` } `json:"nodes"`
	} `json:"vRefs"`
	NvRefs *struct {
		Nodes []struct{ Name string `json:"name"` } `json:"nodes"`
	} `json:"nvRefs"`
}

func BatchLatestVersions(items []BatchItem, cacheTTL string) []BatchResult {
	results := make([]BatchResult, len(items))

	for start := 0; start < len(items); start += batchSize {
		end := min(start+batchSize, len(items))
		batchResults := executeBatch(items[start:end], cacheTTL)
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
		fmt.Fprintf(&b, "  %s: repository(owner: %q, name: %q) {\n", aliases[i], pair[0], pair[1])
		if item.Pin.Raw != "" {
			vp, nvp := tagRefPrefixes(item.Pin)
			fmt.Fprintf(&b, "    vRefs: refs(refPrefix: %q, orderBy: {field: TAG_COMMIT_DATE, direction: DESC}, first: 100) { nodes { name } }\n", vp)
			fmt.Fprintf(&b, "    nvRefs: refs(refPrefix: %q, orderBy: {field: TAG_COMMIT_DATE, direction: DESC}, first: 100) { nodes { name } }\n", nvp)
		} else {
			b.WriteString("    latestRelease { tagName }\n")
		}
		b.WriteString("  }\n")
	}
	b.WriteString("}")
	return b.String()
}

// tagRefPrefixes returns the v-prefixed and non-v-prefixed tag ref prefixes for a constraint.
// e.g. PinMajor{20} → "refs/tags/v20.", "refs/tags/20."
// e.g. PinMinor{20,9} → "refs/tags/v20.9.", "refs/tags/20.9."
func tagRefPrefixes(c config.Constraint) (vPfx, nvPfx string) {
	var base string
	if c.Level == config.PinMinor {
		base = fmt.Sprintf("%d.%d.", c.Major, c.Minor)
	} else {
		base = fmt.Sprintf("%d.", c.Major)
	}
	return "refs/tags/v" + base, "refs/tags/" + base
}

func extractTag(rf releaseField, pin config.Constraint) (string, error) {
	if pin.Raw == "" {
		if rf.LatestRelease == nil {
			return "", fmt.Errorf("no latest release returned")
		}
		return rf.LatestRelease.TagName, nil
	}

	var tags []string
	if rf.VRefs != nil {
		for _, n := range rf.VRefs.Nodes {
			tags = append(tags, n.Name)
		}
	}
	if rf.NvRefs != nil {
		for _, n := range rf.NvRefs.Nodes {
			tags = append(tags, n.Name)
		}
	}

	bestTag := ""
	for _, tag := range tags {
		if isPrereleaseName(tag) {
			continue
		}
		if !pin.Matches(tag) {
			continue
		}
		if bestTag == "" || config.CompareVersions(tag, bestTag) > 0 {
			bestTag = tag
		}
	}
	if bestTag == "" {
		return "", fmt.Errorf("no release found matching %q", pin.Raw)
	}
	return bestTag, nil
}

// isPrereleaseName reports whether a tag looks like a pre-release.
// Standard semver uses a hyphen separator for pre-release identifiers (e.g. v1.0.0-rc.1).
func isPrereleaseName(tag string) bool {
	return strings.Contains(config.NormalizeVersion(tag), "-")
}
