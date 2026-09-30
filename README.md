# ghpm

A package manager that installs portable apps from GitHub Releases, using `gh` as its primary interface to GitHub.

## Install

**Linux / macOS:**

```sh
curl --fail-with-body --location --url https://raw.githubusercontent.com/meop/ghpm/main/install.sh | sh
```

**Windows:**

```powershell
irm -ErrorAction Stop -Uri https://raw.githubusercontent.com/meop/ghpm/main/install.ps1 | iex
```

**From source:**

```sh
go install github.com/meop/ghpm/cmd/ghpm@latest
```

After installing, add `~/.ghpm/bin` to your PATH.

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

ghpm upgrade              # upgrade ghpm itself
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
| `fzf` | Latest version | Yes — `ghpm sync` fetches newest release |
| `fzf@14` | Latest 14.x | Yes — within major only |
| `fzf@14.1` | Latest 14.1.x | Yes — within major.minor only |
| `fzf@14.1.0` | Exact version | Never — static pin |

A pinned package keeps the constraint as its name (e.g., `fzf@14`, not `fzf@14.2.1`); `ghpm list` shows the version installed.

### Portable app support

ghpm finds the binaries in a release's archive on its own, and puts each on your PATH through `~/.ghpm/bin`. GitHub releases are portable apps, so nothing else needs setting up.

You can select **multiple assets** from a single release, for builds split across assets — e.g. one whose shared libraries ship in a separate archive that must sit beside the executables. Later selections win where files collide.

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

Package names like `fzf` are resolved to GitHub repos via `~/.ghpm/repo/`. Any `repo.toml` file anywhere in that directory tree contributes to the map — files are merged alphabetically by path, with later files taking precedence on conflicts. Invalid TOML is a fatal error. Each file is one section per package:

```toml
[fzf]
uri = "github.com/junegunn/fzf"
descr = "Command-line fuzzy finder."
```

`uri` is the `github.com/owner/repo` path. `descr` is optional — one sentence about the tool, shown as a column by `ghpm find` and under the header by `ghpm info`. The older flat form (`fzf = "github.com/junegunn/fzf"`) is still read, so an existing personal `repo.toml` keeps working as is.

`ghpm refresh` fetches `repo.toml` files from the sources in `repo_sources` and writes them into `~/.ghpm/repo/`. You can also place your own `repo.toml` files there in any layout — `~/.ghpm/repo/` is never touched by `ghpm tidy` and is managed manually (or via `ghpm refresh`).

If a name isn't in the map, `ghpm` searches GitHub and prompts you to pick a repo.

## Behavior

- Each downloaded asset's SHA256 is checked against the digest GitHub reports; a mismatch fails the install (bypass with `--skip-hash-check`)
- `sync` keeps the choices you made at install time — which binaries to add, what to name them — as long as a release offers the same binaries and fonts; if that set changes, it asks again from scratch
- A package that fails partway is rolled back to the version you had, so re-running the command simply retries it; other packages in the same run are unaffected

## Verifying releases

Release archives contain `LICENSE`. Verify the signed checksum manifest
with the public release key before checking downloaded artifacts:

```sh
gpg --verify SHA256SUMS.sig SHA256SUMS
sha256sum --ignore-missing --check SHA256SUMS
```

## License

[MIT](LICENSE)
