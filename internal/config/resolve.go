package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/meop/ghpm/internal/ghbin"
	"github.com/meop/ghpm/internal/store"
	"github.com/meop/ghpm/internal/ui"
)

// LoadRepos globs ~/.ghpm/repo recursively for repo.toml files, processes them
// in alphabetical path order (later files overwrite earlier on key conflicts),
// and returns the merged map. Each file is a flat table of name = "source"
// pairs (no top-level key). Returns an empty map if the directory doesn't
// exist. Returns an error if any repo.toml is unreadable or invalid TOML.
func LoadRepos() (map[string]string, error) {
	base, err := store.ReposBaseDir()
	if err != nil {
		return nil, err
	}
	var paths []string
	err = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !d.IsDir() && d.Name() == "repo.toml" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	merged := map[string]string{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var m map[string]string
		if err := toml.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("malformed %s: %w", path, err)
		}
		for k, v := range m {
			if k == "" || v == "" {
				continue
			}
			merged[k] = v
		}
	}
	return merged, nil
}

// RefreshRepos fetches repo.toml from each source configured in settings
// SyncResult holds the outcome of syncing a single repo source.
type SyncResult struct {
	Source string
	Count  int
	Err    error
}

// Called during ghpm update.
func RefreshRepos() ([]SyncResult, error) {
	cfg, err := LoadSettings()
	if err != nil {
		return nil, err
	}
	sources := cfg.RepoSources
	if len(sources) == 0 {
		sources = defaultSettings().RepoSources
	}
	results := make([]SyncResult, 0, len(sources))
	var fetchErrs []string
	for _, source := range sources {
		count, err := fetchAndCacheRepos(source)
		results = append(results, SyncResult{Source: source, Count: count, Err: err})
		if err != nil {
			fetchErrs = append(fetchErrs, fmt.Sprintf("%s: %v", source, err))
		}
	}
	if len(fetchErrs) > 0 {
		return results, fmt.Errorf("%s", strings.Join(fetchErrs, "; "))
	}
	return results, nil
}

func fetchAndCacheRepos(source string) (int, error) {
	slug := strings.TrimPrefix(source, "github.com/")
	if !strings.Contains(slug, "/") {
		return 0, fmt.Errorf("invalid repo source %q (want github.com/owner/repo)", source)
	}
	ghPath, err := ghbin.Find()
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(ghPath, "api", //nolint:gosec
		fmt.Sprintf("repos/%s/contents/repo.toml", slug),
		"--header", "Accept: application/vnd.github.raw+json",
	)
	data, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return 0, fmt.Errorf("gh api: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return 0, err
	}
	var m map[string]string
	if err := toml.Unmarshal(data, &m); err != nil {
		return 0, fmt.Errorf("parsing repo.toml from %s: %w", source, err)
	}
	dir, err := store.RepoDir(source)
	if err != nil {
		return 0, err
	}
	path := filepath.Join(dir, "repo.toml")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	return len(m), nil
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
	"gh": RepoGh.URI,
}

// LookupSource resolves a name from non-interactive sources only.
// Returns the source and true if found; empty string and false if not.
func LookupSource(name string, manifest *Manifest, repos map[string]string) (string, bool) {
	if src, ok := manifest.Repos[name]; ok {
		return src, true
	}
	if src, ok := builtinRepos[name]; ok {
		return src, true
	}
	if repos != nil {
		if src, ok := repos[name]; ok {
			return normalizeSource(src), true
		}
	}
	return "", false
}

// ResolveSource resolves a simple name to a full GitHub URI (github.com/owner/repo).
// Resolution order: manifest repos → builtin repos → user repos → gh search fallback.
// name must have already been validated by ValidateName.
func ResolveSource(name, version string, manifest *Manifest, repos map[string]string) (string, error) {
	if src, found := LookupSource(name, manifest, repos); found {
		return src, nil
	}
	return SearchGitHub(name)
}

// FindBySource returns the name already registered with source.
func (m *Manifest) FindBySource(source string) (string, bool) {
	for name, src := range m.Repos {
		if src == source {
			return name, true
		}
	}
	return "", false
}

// SearchGitHub runs `gh search repos` and prompts the user to pick a result.
func SearchGitHub(name string) (string, error) {
	ghPath, err := ghbin.Find()
	if err != nil {
		return "", err
	}
	out, err := exec.Command(ghPath, "search", "repos", name, "--limit", "5", "--json", "fullName").Output() //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("gh search failed — is gh authenticated?")
	}

	var repos []struct {
		FullName string `json:"fullName"`
	}
	if err := json.Unmarshal(out, &repos); err != nil || len(repos) == 0 {
		return "", fmt.Errorf("no results found for %q", name)
	}

	names := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.FullName
	}
	return ui.Prompt(func() (string, error) {
		ui.Menu("", "repo search results:", names)
		idx, err := ui.ReadSingle("select a repo")
		if err != nil || idx < 1 || idx > len(repos) {
			return "", fmt.Errorf("skipped")
		}
		return "github.com/" + repos[idx-1].FullName, nil
	})
}

func normalizeSource(s string) string {
	if strings.HasPrefix(s, "github.com/") {
		return s
	}
	return "github.com/" + s
}
