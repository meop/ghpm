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
)

const aliasesURL = "https://raw.githubusercontent.com/meop/ghpm-config/main/cfg/aliases.yaml"

type aliasesFile struct {
	Aliases map[string]string `yaml:"aliases"`
}

func aliasesCachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ghpm", "aliases.yaml"), nil
}

// LoadAliases reads the locally cached aliases from ~/.ghpm/aliases.yaml.
// Returns nil (no error) if the cache doesn't exist yet.
func LoadAliases() (map[string]string, error) {
	path, err := aliasesCachePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var af aliasesFile
	if err := yaml.Unmarshal(data, &af); err != nil {
		return nil, err
	}
	return af.Aliases, nil
}

// RefreshAliases fetches the remote aliases.yaml and saves it to
// ~/.ghpm/aliases.yaml for local caching. Called during ghpm update.
func RefreshAliases() (map[string]string, error) {
	resp, err := http.Get(aliasesURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var af aliasesFile
	if err := yaml.Unmarshal(data, &af); err != nil {
		return nil, err
	}
	if err := saveAliasesCache(data); err != nil {
		return nil, err
	}
	return af.Aliases, nil
}

func saveAliasesCache(data []byte) error {
	path, err := aliasesCachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
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

// ResolveSource resolves a simple package name to a full GitHub URI (github.com/owner/repo).
// Resolution order: manifest → aliases → gh search fallback.
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

	// 2. Aliases (from local cache)
	if aliases != nil {
		if src, ok := aliases[name]; ok {
			return normalizeSource(src), nil
		}
	}

	// 3. GitHub search fallback
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
