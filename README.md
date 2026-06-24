# ghpm

A package manager that installs portable apps from GitHub Releases, using `gh` as its primary interface to GitHub.

## Install

**Linux / macOS:**

```sh
curl --fail-with-body --location --no-progress-meter --url https://raw.githubusercontent.com/meop/ghpm/main/install.sh | sh
```

**Windows:**

```powershell
irm -ErrorAction Stop -ProgressAction SilentlyContinue -Uri https://raw.githubusercontent.com/meop/ghpm/main/install.ps1 | iex
```

**From source:**

```sh
go install github.com/meop/ghpm/cmd/ghpm@latest
```

After installing, add `~/.ghpm/bin` to your PATH. Each installed binary gets a shim there — a symlink on Linux/macOS, an `.exe` shim on Windows.

## Usage

```sh
ghpm add fzf              # install latest fzf
ghpm add fzf@14           # install latest fzf 14.x (tracks within major)
ghpm add fzf@14.1         # install latest fzf 14.1.x (tracks within minor)
ghpm add fzf@14.1.0       # install exact fzf 14.1.0 (static, never updates)
ghpm add fzf ripgrep bat  # install multiple in parallel
ghpm add --force fzf      # reinstall even if already installed

ghpm list                 # show installed packages
ghpm list fzf ripgrep     # show only the named packages
ghpm find fzf             # search cached repos by name or source
ghpm info fzf             # show available releases and assets
ghpm outdated             # check for updates
ghpm outdated fzf         # check updates for only the named packages

ghpm sync                 # update all floating and major/minor-pinned packages
ghpm sync fzf             # update specific package
ghpm sync --force         # reinstall all packages even if already at latest version
ghpm sync --force fzf     # reinstall specific package even if already at latest version

ghpm download fzf         # download release asset to cache without installing
ghpm download --path /tmp fzf # download release asset to a specific directory

ghpm remove fzf           # remove package
ghpm tidy                 # remove unused cached assets and orphaned package dirs
ghpm tidy --all           # remove all cached assets

ghpm upgrade              # upgrade ghpm itself and managed gh
ghpm refresh              # refresh repo sources to latest versions
ghpm doctor               # check system health
```

### Global flags

| Flag | Description |
|---|---|
| `--dry-run`, `-n` | Print what would be done without executing |
| `--yes`, `-y` | Skip confirmation prompts |

### Per-command flags

| Flag | Commands | Description |
|---|---|---|
| `--force`, `-f` | `add`, `sync` | Reinstall even if already installed / already at latest version |
| `--path` | `download` | Destination directory (default: `~/.ghpm/download/`) |
| `--all` | `tidy` | Remove all cached assets regardless of installation status |
| `--long-names`, `-l` | `list`, `find`, `outdated` | Print names only, one per line |
| `--short-names`, `-s` | `list`, `find`, `outdated` | Print names only, space-separated on one line |
| `--skip-hash-check` | `add`, `sync`, `download`, `upgrade` | Skip SHA256 hash verification of downloaded assets |

### Version pinning

| Syntax | Meaning | Updates |
|---|---|---|
| `fzf` | Latest version | Yes — `ghpm update` fetches newest release |
| `fzf@14` | Latest 14.x | Yes — within major only |
| `fzf@14.1` | Latest 14.1.x | Yes — within major.minor only |
| `fzf@14.1.0` | Exact version | Never — static pin |

Manifest key and directory name both use the constraint as written (e.g., `fzf@14`, not `fzf@14.2.1`). The actual installed version is recorded in the manifest.

### Portable app support

ghpm extracts archives into `~/.ghpm/extract/<key>/<version>/` and discovers the binary automatically. A shim is created in `~/.ghpm/bin/` pointing at the real binary inside the extract dir — a symlink on Linux/macOS, an `.exe` shim on Windows. GitHub releases are portable apps — binaries locate their own resources via paths relative to the executable, so no other env vars are needed.

You can select **multiple assets** from a single release; they are overlaid into one extract dir in selection order (a later asset overwrites a colliding path), then binaries and fonts are discovered across the combined tree. This handles releases split across assets — e.g. a build whose shared libraries ship in a separate archive that must sit beside the executables.

### Configuration

`~/.ghpm/config.toml` is **not created by default** — ghpm runs fine without it. Create it to override any of these defaults:

```toml
cache_ttl = "5m"
no_color = false
num_parallel = 5
repo_sources = ["github.com/meop/ghpm-config"]
skip_hash_check = false

[color]
fail = "red"
info = "blue"
new = "cyan"
old = "magenta"
pass = "green"
warn = "yellow"
```

| Field | Default | Description |
|---|---|---|
| `cache_ttl` | `"5m"` | How long cached version data stays fresh before re-fetching |
| `color` | see above | Output colors by message type |
| `no_color` | `false` | Disable colored output |
| `num_parallel` | `5` | Max concurrent downloads |
| `repo_sources` | `["github.com/meop/ghpm-config"]` | Remote sources `ghpm refresh` fetches `repo.toml` files from |
| `skip_hash_check` | `false` | Permanently skip SHA256 hash verification (same as always passing `--skip-hash-check`) |

### Repo map

Package names like `fzf` are resolved to GitHub repos via `~/.ghpm/repo/`. Any `repo.toml` file anywhere in that directory tree contributes to the map — files are merged alphabetically by path, with later files taking precedence on conflicts. Invalid TOML is a fatal error. Each file is a flat table of `name = "source"` pairs (no top-level key):

```toml
fzf = "github.com/junegunn/fzf"
rg = "github.com/BurntSushi/ripgrep"
```

`ghpm refresh` fetches `repo.toml` files from the sources in `repo_sources` and writes them into `~/.ghpm/repo/`. You can also place your own `repo.toml` files there in any layout — `~/.ghpm/repo/` is never touched by `ghpm tidy` and is managed manually (or via `ghpm refresh`).

If a name isn't in the map, `ghpm` searches GitHub and prompts you to pick a repo.

## How it works

- All GitHub interaction goes through the `gh` CLI — no GitHub SDK
- Release assets are cached in `~/.ghpm/download/github.com/<owner>/<repo>/<version>/`
- Packages are extracted to `~/.ghpm/extract/<key>/<version>/` with full directory structure
- A shim in `~/.ghpm/bin/` points at the binary in each package's extract dir
- State is tracked in `~/.ghpm/manifest.json`
- SHA256 of each downloaded asset is verified against the digest returned by the GitHub API; mismatch is a hard error (bypass with `--skip-hash-check`)

## License

[MIT](LICENSE.txt)
