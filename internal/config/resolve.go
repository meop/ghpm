package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/meop/ghpm/internal/ghbin"
	"github.com/meop/ghpm/internal/parallel"
	"github.com/meop/ghpm/internal/store"
	"github.com/meop/golie/ui"
)

// RepoEntry is one registry entry: where a package lives, a one-sentence
// description of it for find and info to show, and any other names it goes by.
// The entry's key is the name ghpm installs it under; an alias is a second way
// to say that name (ripgrep for rg), never a second name to install it as.
type RepoEntry struct {
	URI     string
	Descr   string
	Aliases []string
}

// parseRepoTable decodes one repo.toml. Both entry shapes are accepted:
//
//	fzf = "github.com/junegunn/fzf"                       # legacy, uri only
//	[rg]                                                  # current
//	uri = "github.com/BurntSushi/ripgrep"
//	descr = "Line-oriented regex search tool."
//	aliases = ["ripgrep"]
//
// Unknown keys inside an entry are ignored rather than rejected, so a registry
// that starts publishing a new field does not break the ghpm versions that
// predate it. path only names the file in error messages.
func parseRepoTable(data []byte, path string) (map[string]RepoEntry, error) {
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("malformed %s: %w", path, err)
	}
	entries := make(map[string]RepoEntry, len(raw))
	for name, v := range raw {
		switch t := v.(type) {
		case string:
			entries[name] = RepoEntry{URI: t}
		case map[string]any:
			uri, _ := t["uri"].(string)
			descr, _ := t["descr"].(string)
			var aliases []string
			if list, ok := t["aliases"].([]any); ok {
				for _, a := range list {
					if alias, ok := a.(string); ok && alias != "" {
						aliases = append(aliases, alias)
					}
				}
			}
			entries[name] = RepoEntry{URI: uri, Descr: descr, Aliases: aliases}
		default:
			return nil, fmt.Errorf("malformed %s: entry %q must be a uri string or a table", path, name)
		}
	}
	return entries, nil
}

// LoadRepos globs ~/.ghpm/repo recursively for repo.toml files, processes them
// in alphabetical path order (later files overwrite earlier on key conflicts),
// and returns the merged map. Returns an empty map if the directory doesn't
// exist. Returns an error if any repo.toml is unreadable or invalid TOML.
func LoadRepos() (map[string]RepoEntry, error) {
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
	merged := map[string]RepoEntry{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		entries, err := parseRepoTable(data, path)
		if err != nil {
			return nil, err
		}
		for k, v := range entries {
			if k == "" || v.URI == "" {
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

// Called by ghpm refresh.
func RefreshRepos() ([]SyncResult, error) {
	cfg, err := LoadSettings()
	if err != nil {
		return nil, err
	}
	sources := cfg.RepoSources
	if len(sources) == 0 {
		sources = defaultSettings().RepoSources
	}

	// One source per task, run through the same bounded pool the rest of the
	// app uses for independent network fetches, rather than fetching sources
	// one at a time.
	tasks := make([]parallel.Task[SyncResult], len(sources))
	for i, source := range sources {
		tasks[i] = parallel.Task[SyncResult]{
			Name: source,
			Run: func() (SyncResult, error) {
				count, err := fetchAndCacheRepos(source)
				return SyncResult{Source: source, Count: count, Err: err}, nil
			},
		}
	}

	parResults := parallel.Run(context.Background(), tasks, cfg.NumParallel)
	results := make([]SyncResult, len(parResults))
	var fetchErrs []string
	for i, res := range parResults {
		results[i] = res.Value
		if res.Value.Err != nil {
			fetchErrs = append(fetchErrs, fmt.Sprintf("%s: %v", res.Value.Source, res.Value.Err))
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
	cmd, err := ghbin.Command("api",
		fmt.Sprintf("repos/%s/contents/repo.toml", slug),
		"--header", "Accept: application/vnd.github.raw+json",
	)
	if err != nil {
		return 0, err
	}
	data, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return 0, fmt.Errorf("gh api: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return 0, err
	}
	entries, err := parseRepoTable(data, "repo.toml from "+source)
	if err != nil {
		return 0, err
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
	return len(entries), nil
}

// ParseVersionSuffix splits "fzf@0.70" → ("fzf", "0.70", true).
// If no "@", returns (name, "", false).
func ParseVersionSuffix(arg string) (name, version string, pinned bool) {
	if before, after, found := strings.CutLast(arg, "@"); found {
		return before, after, true
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

// CanonicalName turns what was typed into the name ghpm knows the package by:
// the manifest's or the registry's own key for it. A name is taken whole — a key
// as it is, or an alias of one, case aside — and never as part of one, since
// every command that reaches here is about to act on what it names. A name
// that is nobody's comes back unchanged, for the caller to report or search.
// Keys win over aliases, and the registry is walked in sorted order, so an
// alias two entries both claim resolves the same way on every run.
func CanonicalName(name string, manifest *Manifest, repos map[string]RepoEntry) string {
	if manifest != nil {
		if _, ok := manifest.Repos[name]; ok {
			return name
		}
	}
	if _, ok := builtinRepos[name]; ok {
		return name
	}
	if _, ok := repos[name]; ok {
		return name
	}
	keys := make([]string, 0, len(repos))
	for k := range repos {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if manifest != nil {
		for k := range manifest.Repos {
			if SameName(k, name) {
				return k
			}
		}
	}
	for _, k := range keys {
		if SameName(k, name) {
			return k
		}
	}
	for _, k := range keys {
		for _, alias := range repos[k].Aliases {
			if SameName(alias, name) {
				return k
			}
		}
	}
	return name
}

// LookupSource resolves a name from non-interactive sources only.
// Returns the source and true if found; empty string and false if not.
func LookupSource(name string, manifest *Manifest, repos map[string]RepoEntry) (string, bool) {
	if src, ok := manifest.Repos[name]; ok {
		return src, true
	}
	if src, ok := builtinRepos[name]; ok {
		return src, true
	}
	if repos != nil {
		if entry, ok := repos[name]; ok && entry.URI != "" {
			return normalizeSource(entry.URI), true
		}
	}
	return "", false
}

// ResolveSource resolves a simple name to a full GitHub URI (github.com/owner/repo).
// Resolution order: manifest repos → builtin repos → user repos → gh search fallback.
// name must have already been validated by ValidateName.
func ResolveSource(name, version string, manifest *Manifest, repos map[string]RepoEntry) (string, error) {
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

// RepoHit is one result of a GitHub repository search.
type RepoHit struct {
	FullName string `json:"fullName"`
	Descr    string `json:"description"`
}

// GitHub's search ranks by relevance and decides for itself what a term means,
// so its answer is only ever a set of candidates: the two rules every other
// name goes through (SameName, WordStartsWith) decide which of them count. More
// is fetched than is shown, since most of what comes back is dropped.
const (
	searchFetch = 50
	searchShow  = 10
)

// searchRepos runs `gh search repos` for term, looked for in the given fields
// of a repository (name, description). It is the deeper search, the one past
// the registry: GitHub is asked only about a name nothing in repo.toml
// answered to.
func searchRepos(term, in string) ([]RepoHit, error) {
	cmd, err := ghbin.Command("search", "repos", "--limit", strconv.Itoa(searchFetch),
		"--match", in, "--json", "fullName,description", "--", term)
	if err != nil {
		return nil, err
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh search failed — is gh authenticated?")
	}
	var hits []RepoHit
	if err := json.Unmarshal(out, &hits); err != nil {
		return nil, fmt.Errorf("gh search failed: %w", err)
	}
	return hits, nil
}

// HitsNamed keeps the repos whose own name is the term, whoever owns them: an
// acting command's question (SameName), put to GitHub's answer. "fzf" keeps
// junegunn/fzf and never junegunn/fzf.vim.
func HitsNamed(hits []RepoHit, name string) []RepoHit {
	return keepHits(hits, func(h RepoHit) bool {
		_, repo, _ := strings.Cut(h.FullName, "/")
		return SameName(repo, name)
	})
}

// HitsFound keeps the repos the term starts a word of, in owner/repo or in the
// description: a reading command's question (WordStartsWith), put to GitHub's
// answer.
func HitsFound(hits []RepoHit, term string) []RepoHit {
	return keepHits(hits, func(h RepoHit) bool {
		return WordStartsWith(h.FullName, term) || WordStartsWith(h.Descr, term)
	})
}

func keepHits(hits []RepoHit, keep func(RepoHit) bool) []RepoHit {
	var kept []RepoHit
	for _, h := range hits {
		if keep(h) && len(kept) < searchShow {
			kept = append(kept, h)
		}
	}
	return kept
}

// SearchReposNamed is add's and info's deeper search: the repos on GitHub named
// exactly this. An acting command takes a name whole from the registry, and
// takes it whole from GitHub too, so a vague term is find's to narrow.
func SearchReposNamed(name string) ([]RepoHit, error) {
	hits, err := searchRepos(name, "name")
	if err != nil {
		return nil, err
	}
	named := HitsNamed(hits, name)
	if len(named) == 0 {
		return nil, fmt.Errorf("no github repo named %q — ghpm find %s looks wider", name, name)
	}
	return named, nil
}

// SearchReposFound is find's deeper search: the repos on GitHub the term starts
// a word of, by name or description. None is an answer, not an error.
func SearchReposFound(term string) ([]RepoHit, error) {
	hits, err := searchRepos(term, "name,description")
	if err != nil {
		return nil, err
	}
	return HitsFound(hits, term), nil
}

// SearchGitHub runs SearchReposNamed and prompts the user to pick a result:
// the name was exact, and the pick says whose repo of that name is meant.
// With nobody to ask, an unknown name is simply not found: picking a repo out
// of a search on a script's behalf is how `info <typo>` ends up reporting on
// somebody else's project.
func SearchGitHub(name string) (string, error) {
	if !ui.Interactive() {
		return "", fmt.Errorf("not found: %q", name)
	}
	repos, err := SearchReposNamed(name)
	if err != nil {
		return "", err
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
