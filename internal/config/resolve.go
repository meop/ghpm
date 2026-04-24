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

type reposFile struct {
	Repos map[string]string `yaml:"repos"`
}

// LoadRepos scans ~/.ghpm/repos recursively for repos.yaml files,
// loads all of them, and merges into a single map (later entries win on conflict).
// Returns an empty map (no error) if the repos directory doesn't exist yet.
func LoadRepos() (map[string]string, error) {
	base, err := store.ReposBaseDir()
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
		if d.IsDir() || d.Name() != "repos.yaml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", path, err)
			return nil
		}
		var rf reposFile
		if err := yaml.Unmarshal(data, &rf); err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping malformed %s: %v\n", path, err)
			return nil
		}
		for k, v := range rf.Repos {
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

// RefreshRepos fetches repos.yaml from each source configured in settings
// (default: github.com/meop/ghpm-config) and caches it under ~/.ghpm/repos/.
// Called during ghpm update.
func RefreshRepos() (map[string]string, error) {
	cfg, err := LoadSettings()
	if err != nil {
		return nil, err
	}
	sources := cfg.RepoSources
	if len(sources) == 0 {
		sources = defaultSettings().RepoSources
	}
	var fetchErrs []string
	for _, source := range sources {
		if err := fetchAndCacheRepos(source); err != nil {
			fetchErrs = append(fetchErrs, fmt.Sprintf("%s: %v", source, err))
		}
	}
	if len(fetchErrs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(fetchErrs, "; "))
	}
	return LoadRepos()
}

func fetchAndCacheRepos(source string) error {
	slug := strings.TrimPrefix(source, "github.com/")
	if !strings.Contains(slug, "/") {
		return fmt.Errorf("invalid repo source %q (want github.com/owner/repo)", source)
	}
	url := fmt.Sprintf("https://raw.githubusercontent.com/%s/main/repos.yaml", slug)
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
	var rf reposFile
	if err := yaml.Unmarshal(data, &rf); err != nil {
		return fmt.Errorf("parsing repos.yaml from %s: %w", source, err)
	}
	dir, err := store.RepoDir(source)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "repos.yaml")
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

// builtinRepos maps names that ghpm always knows about without user repos or search.
var builtinRepos = map[string]string{
	"gh": "github.com/cli/cli",
}

// ResolveSource resolves a simple name to a full GitHub URI (github.com/owner/repo).
// Resolution order: manifest repos → builtin repos → user repos → gh search fallback.
// name must have already been validated by ValidateName.
func ResolveSource(name, version string, manifest *Manifest, repos map[string]string) (string, error) {
	// 1. Manifest repos (already-installed)
	if src, ok := manifest.Repos[name]; ok {
		return src, nil
	}

	// 2. Builtin repos (well-known)
	if src, ok := builtinRepos[name]; ok {
		return src, nil
	}

	// 3. User repos (from local cache)
	if repos != nil {
		if src, ok := repos[name]; ok {
			return normalizeSource(src), nil
		}
	}

	// 4. GitHub search fallback
	return searchGitHub(name)
}

// FindBySource returns the name already registered with source.
func FindBySource(source string, manifest *Manifest) (string, bool) {
	for name, src := range manifest.Repos {
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
