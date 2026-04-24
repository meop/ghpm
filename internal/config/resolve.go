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

type aliasesFile struct {
	Aliases map[string]string `yaml:"aliases"`
}

// LoadAliases scans ~/.ghpm/aliases recursively for aliases.yaml files,
// loads all of them, and merges into a single map (later entries win on conflict).
// Returns an empty map (no error) if the aliases directory doesn't exist yet.
func LoadAliases() (map[string]string, error) {
	base, err := store.AliasesBaseDir()
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
		if d.IsDir() || d.Name() != "aliases.yaml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", path, err)
			return nil
		}
		var af aliasesFile
		if err := yaml.Unmarshal(data, &af); err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping malformed %s: %v\n", path, err)
			return nil
		}
		for k, v := range af.Aliases {
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

// RefreshAliases fetches aliases.yaml from each repo configured in settings
// (default: github.com/meop/ghpm-config) and caches it under ~/.ghpm/aliases/.
// Called during ghpm update.
func RefreshAliases() (map[string]string, error) {
	cfg, err := LoadSettings()
	if err != nil {
		return nil, err
	}
	repos := cfg.AliasRepos
	if len(repos) == 0 {
		repos = defaultSettings().AliasRepos
	}
	var fetchErrs []string
	for _, repo := range repos {
		if err := fetchAndCacheAliases(repo); err != nil {
			fetchErrs = append(fetchErrs, fmt.Sprintf("%s: %v", repo, err))
		}
	}
	if len(fetchErrs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(fetchErrs, "; "))
	}
	return LoadAliases()
}

func fetchAndCacheAliases(repo string) error {
	slug := strings.TrimPrefix(repo, "github.com/")
	if !strings.Contains(slug, "/") {
		return fmt.Errorf("invalid alias repo %q (want github.com/owner/repo)", repo)
	}
	url := fmt.Sprintf("https://raw.githubusercontent.com/%s/main/aliases.yaml", slug)
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
	var af aliasesFile
	if err := yaml.Unmarshal(data, &af); err != nil {
		return fmt.Errorf("parsing aliases.yaml from %s: %w", repo, err)
	}
	dir, err := store.AliasDir(repo)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "aliases.yaml")
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

// builtinSources maps names that ghpm always knows about without aliases or search.
var builtinSources = map[string]string{
	"gh": "github.com/cli/cli",
}

// ResolveSource resolves a simple package name to a full GitHub URI (github.com/owner/repo).
// Resolution order: manifest → builtins → aliases → gh search fallback.
// name must have already been validated by ValidateName.
func ResolveSource(name, version string, manifest *Manifest, aliases map[string]string) (string, error) {
	// 1. Manifest lookup
	if version != "" {
		if p, ok := manifest.Packages[name+"@"+version]; ok {
			return p.Source, nil
		}
	}
	if p, ok := manifest.Packages[name]; ok {
		return p.Source, nil
	}

	// 2. Builtins
	if src, ok := builtinSources[name]; ok {
		return src, nil
	}

	// 3. Aliases (from local cache)
	if aliases != nil {
		if src, ok := aliases[name]; ok {
			return normalizeSource(src), nil
		}
	}

	// 4. GitHub search fallback
	return searchGitHub(name)
}

// FindBySource returns the manifest key of any package already installed from source.
func FindBySource(source string, manifest *Manifest) (string, bool) {
	for key, pkg := range manifest.Packages {
		if pkg.Source == source {
			return key, true
		}
	}
	return "", false
}

// searchGitHub runs `gh search repos` and prompts the user to pick a result.
func searchGitHub(name string) (string, error) {
	out, err := exec.Command("gh", "search", "repos", name, "--limit", "5", "--json", "fullName").Output()
	if err != nil {
		return "", fmt.Errorf("no alias for %q and gh search failed — is gh authenticated?", name)
	}

	var repos []struct {
		FullName string `json:"fullName"`
	}
	if err := json.Unmarshal(out, &repos); err != nil || len(repos) == 0 {
		return "", fmt.Errorf("no results found for %q", name)
	}

	fmt.Printf("No alias for %q. GitHub search results:\n", name)
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
