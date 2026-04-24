package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/meop/ghpm/internal/store"
)

type toolsFile struct {
	Tools map[string]string `yaml:"tools"`
}

// LoadTools scans ~/.ghpm/tools recursively for tools.yaml files,
// loads all of them, and merges into a single map (later entries win on conflict).
// Returns an empty map (no error) if the tools directory doesn't exist yet.
func LoadTools() (map[string]string, error) {
	base, err := store.ToolsBaseDir()
	if err != nil {
		return nil, err
	}
	merged := map[string]string{}
	err = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() || d.Name() != "tools.yaml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", path, err)
			return nil
		}
		var tf toolsFile
		if err := yaml.Unmarshal(data, &tf); err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping malformed %s: %v\n", path, err)
			return nil
		}
		for k, v := range tf.Tools {
			if k == "" || v == "" {
				continue
			}
			merged[k] = v
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return merged, nil
}

// RefreshTools fetches tools.yaml from each repo configured in settings
// (default: github.com/meop/ghpm-config) and caches it under ~/.ghpm/tools/.
// Called during ghpm update.
func RefreshTools() (map[string]string, error) {
	cfg, err := LoadSettings()
	if err != nil {
		return nil, err
	}
	repos := cfg.ToolRepos
	if len(repos) == 0 {
		repos = defaultSettings().ToolRepos
	}
	var fetchErrs []string
	for _, repo := range repos {
		if err := fetchAndCacheTools(repo); err != nil {
			fetchErrs = append(fetchErrs, fmt.Sprintf("%s: %v", repo, err))
		}
	}
	if len(fetchErrs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(fetchErrs, "; "))
	}
	return LoadTools()
}

func fetchAndCacheTools(repo string) error {
	slug := strings.TrimPrefix(repo, "github.com/")
	if !strings.Contains(slug, "/") {
		return fmt.Errorf("invalid tool repo %q (want github.com/owner/repo)", repo)
	}
	url := fmt.Sprintf("https://raw.githubusercontent.com/%s/main/tools.yaml", slug)
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, url)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var tf toolsFile
	if err := yaml.Unmarshal(data, &tf); err != nil {
		return fmt.Errorf("parsing tools.yaml from %s: %w", repo, err)
	}
	dir, err := store.ToolDir(repo)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "tools.yaml")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ParseVersionSuffix splits "fzf@0.70" → ("fzf", "0.70", true).
// If no "@", returns (name, "", false).
func ParseVersionSuffix(arg string) (name, version string, pinned bool) {
	if idx := strings.LastIndex(arg, "@"); idx >= 0 {
		return arg[:idx], arg[idx+1:], true
	}
	return arg, "", false
}

// ValidateName returns an error if name is not a simple filename (no slashes, no spaces).
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("package name cannot be empty")
	}
	if strings.ContainsAny(name, "/\\ ") {
		return fmt.Errorf("name must be a simple filename with no slashes or spaces (got %q)\n  hint: use a plain name like 'gh', 'fzf', 'ripgrep'", name)
	}
	return nil
}

// builtinTools maps names that ghpm always knows about without user tools or search.
var builtinTools = map[string]string{
	"gh": "github.com/cli/cli",
}

// ResolveSource resolves a simple tool name to a full GitHub URI (github.com/owner/repo).
// Resolution order: manifest tools → builtin tools → user tools → gh search fallback.
// name must have already been validated by ValidateName.
func ResolveSource(name, version string, manifest *Manifest, tools map[string]string) (string, error) {
	// 1. Manifest tools (already-installed)
	if src, ok := manifest.Tools[name]; ok {
		return src, nil
	}

	// 2. Builtin tools (well-known)
	if src, ok := builtinTools[name]; ok {
		return src, nil
	}

	// 3. User tools (from local cache)
	if tools != nil {
		if src, ok := tools[name]; ok {
			return normalizeSource(src), nil
		}
	}

	// 4. GitHub search fallback
	return searchGitHub(name)
}

// FindBySource returns the tool name already registered with source.
func FindBySource(source string, manifest *Manifest) (string, bool) {
	for name, src := range manifest.Tools {
		if src == source {
			return name, true
		}
	}
	return "", false
}

// searchGitHub runs `gh search repos` and prompts the user to pick a result.
func searchGitHub(name string) (string, error) {
	out, err := exec.Command("gh", "search", "repos", name, "--limit", "5", "--json", "fullName").Output()
	if err != nil {
		return "", fmt.Errorf("no entry for %q and gh search failed — is gh authenticated?", name)
	}

	var repos []struct {
		FullName string `json:"fullName"`
	}
	if err := json.Unmarshal(out, &repos); err != nil || len(repos) == 0 {
		return "", fmt.Errorf("no results found for %q", name)
	}

	fmt.Printf("No entry for %q. GitHub search results:\n", name)
	for i, r := range repos {
		fmt.Printf("  %d) %s\n", i+1, r.FullName)
	}
	fmt.Print("Select a repo (or 0 to cancel): ")

	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(line)
	var idx int
	if _, err := fmt.Sscanf(line, "%d", &idx); err != nil || idx < 1 || idx > len(repos) {
		return "", fmt.Errorf("cancelled")
	}
	return "github.com/" + repos[idx-1].FullName, nil
}

func normalizeSource(s string) string {
	if strings.HasPrefix(s, "github.com/") {
		return s
	}
	return "github.com/" + s
}
