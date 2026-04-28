# ghpm Design Document

## Overview

ghpm is a Go CLI package manager that installs binaries from GitHub Releases, using `gh` as its primary interface to GitHub.

**Repository**: `github.com/meop/ghpm`
**Config repo**: `github.com/meop/ghpm-config` (remote repos, platform priorities)

---

## Project Scaffolding

### Directory layout

```
ghpm/
├── cmd/
│   └── ghpm/
│       └── main.go
├── internal/
│   ├── asset/        # asset matching, selection, extraction, SHA verification, path discovery
│   ├── cli/          # cobra command definitions
│   ├── config/       # manifest, settings, repo resolution, version normalization, process lock
│   ├── entrypoint/   # shell env script generation (sh, nu, ps1) → ~/.ghpm/scripts/env.*
│   ├── gh/           # gh CLI wrapper, GraphQL batch queries, rate limit detection
│   ├── parallel/     # worker pool orchestration
│   └── store/        # path helpers: BinDir, ScriptsDir, PackagesDir, PackageDir for ~/.ghpm/
├── install.sh
├── install.ps1
├── go.mod
├── go.sum
├── DESIGN.md
└── Makefile
```

### Dependencies

| Dependency | Purpose |
|---|---|
| `encoding/json` | Manifest, settings — stdlib only |
| `github.com/fatih/color` | Terminal color output |
| `github.com/gofrs/flock` | Cross-platform file locking (process lock) |
| `github.com/spf13/cobra` | CLI framework (commands, flags, help, completion) |
| `gopkg.in/yaml.v3` | Parse remote repos.yaml from ghpm-config |

No heavy GitHub SDK — all GitHub interaction goes through `gh` CLI invoked via `os/exec`.

---

## Config & Manifest

### Directory structure (`~/.ghpm/`)

```
~/.ghpm/
├── .lock                       # process lock (flock)
├── bin/
│   ├── gh                      # gh CLI (bootstrap dependency, managed by ghpm upgrade)
│   └── ghpm                    # ghpm itself (managed by ghpm upgrade)
├── manifest.json               # tracked packages (gh is NOT in the manifest)
├── packages/                   # installed packages (full archive extracts)
│   ├── fzf/
│   │   └── fzf                 # single binary
│   ├── helix/
│   │   └── helix-24.3-x86_64-linux/
│   │       └── helix-24.3/
│   │           ├── bin/hx
│   │           ├── lib/
│   │           ├── share/
│   │           └── runtime/
│   └── ...
├── releases/                   # cached release assets
│   └── github.com/<owner>/<repo>/<version>/<asset>
├── repos/                      # cached repo files per source
│   └── github.com/<owner>/<repo>/repos.yaml
├── scripts/
│   ├── env.sh                  # generated for bash/zsh (POSIX)
│   ├── env.nu                  # generated for nushell
│   └── env.ps1                 # generated for PowerShell
├── settings.json               # user preferences (optional, not created by default)
```

### Manifest (`manifest.json`)

Plain JSON, fully managed by ghpm:

```json
{
  "repos": {
    "bun": "github.com/oven-sh/bun",
    "fzf": "github.com/junegunn/fzf"
  },
  "installs": {
    "bun": {
      "pin": "latest",
      "version": "1.3.13",
      "asset": "bun-linux-x64.zip",
      "paths": {
        "bin": ["."]
      },
      "binary_name": "bun"
    },
    "helix": {
      "pin": "latest",
      "version": "24.3",
      "asset": "helix-24.3-x86_64-linux.tar.xz",
      "paths": {
        "bin": ["helix-24.3-x86_64-linux/helix-24.3/bin"],
        "lib": ["helix-24.3-x86_64-linux/helix-24.3/lib"],
        "share": ["helix-24.3-x86_64-linux/helix-24.3/share"],
        "man": ["helix-24.3-x86_64-linux/helix-24.3/share/man"]
      },
      "binary_name": "hx"
    },
    "fzf@0.70": {
      "pin": "minor",
      "version": "0.70.0",
      "asset": "fzf-0.70.0-linux_amd64.tar.gz",
      "paths": {
        "bin": ["."]
      },
      "binary_name": "fzf"
    }
  }
}
```

Fields:

- **`pin`**: `"latest"`, `"major"`, `"minor"`, or `"fixed"`. Floating packages track the newest release; pinned packages update within their constraint; exact pins never update.
- **`version`**: installed version, normalized by stripping all leading non-digit characters from the GitHub tag (e.g., `"bun-v1.3.13"` → `"1.3.13"`, `"v0.71.0"` → `"0.71.0"`).
- **`asset`**: the exact asset filename chosen during install. On `update`, ghpm tokenizes both the stored and candidate asset names, strips version-like tokens, and matches on the remaining structural tokens. If exactly one candidate matches, it is auto-selected without prompting.
- **`paths`**: discovered directory paths (relative to `packages/<key>/`) categorized by type. `"bin"` entries are added to `PATH`, `"lib"` to `LD_LIBRARY_PATH`, `"share"` to `XDG_DATA_DIRS`, `"man"` to `MANPATH`.
- **`binary_name`**: the primary executable found in the archive, used for informational purposes.

### Settings (`settings.json`, optional)

`~/.ghpm/settings.json` is **not created by default** — ghpm runs fine without it using hardcoded defaults. Create it to override:

```json
{
  "cache_ttl": "5m",
  "no_verify": false,
  "no_color": false,
  "num_parallel": 5,
  "plat_priority": {
    "linux": ["gnu", "musl"],
    "windows": ["msvc", "gnu"]
  },
  "repo_sources": ["github.com/meop/ghpm-config"]
}
```

- **`cache_ttl`**: duration to cache `gh api` responses, passed as `--cache` flag (default: `"5m"`)
- **`no_color`**: disable colored output (default: false)
- **`no_verify`**: skip SHA verification globally (default: false)
- **`num_parallel`**: max concurrent download/extract operations (default: 5)
- **`plat_priority`**: when multiple assets match OS+arch, prefer toolchains in this order. Default: Windows → MSVC over GNU; Linux → GNU over Musl.
- **`repo_sources`**: list of GitHub repos to fetch `repos.yaml` from (default: `["github.com/meop/ghpm-config"]`). Multiple sources are supported; their repo maps are merged (later entries win on conflict).

### Config module (`internal/config/`)

- `AcquireLock() (func(), error)` — acquire exclusive process lock via `flock`
- `EnsureDirs() error` — create `~/.ghpm/{bin,packages,releases,repos,scripts}` if missing
- `LoadManifest() (*Manifest, error)` — read manifest, create if missing
- `LoadSettings() (*Settings, error)` — load settings with hardcoded defaults for missing file
- `NormalizeVersion(v string) string` — strip all leading non-digit chars from tag
- `SaveManifest(m *Manifest) error` — atomic write (write to temp, rename)

---

## gh CLI Wrapper (`internal/gh/`)

All GitHub interaction goes through `gh`.

### Core functions

| Function | `gh` command | Description |
|---|---|---|
| `BatchLatestVersions(items []BatchItem, cacheTTL string) []BatchResult` | `gh api graphql -f query=... --cache 5m` | Batch-check latest versions for up to 50 repos per call |
| `CheckInstalled() error` | `which gh` | Verify `gh` is available and authenticated |
| `DownloadAsset(owner, repo, tag, pattern, dest string) error` | `gh release download tag -R owner/repo -p pattern -D dest` | Download matching asset(s) |
| `GetLatestRelease(owner, repo string) (Release, error)` | `gh release view -R owner/repo --json tagName,assets` | Get latest release with assets |
| `GetReleaseByTag(owner, repo, tag string) (Release, error)` | `gh release view tag -R owner/repo --json tagName,assets` | Get specific release |
| `IsRateLimited(err error) bool` | — | Check if error is a rate limit response |
| `ListReleases(owner, repo string) ([]Release, error)` | `gh release list -R owner/repo --json tagName,isLatest` | Fetch all releases |

### Data types

```go
type Release struct {
    TagName  string
    Assets   []Asset
    IsLatest bool
}

type Asset struct {
    Name string
    Size int64
    URL  string
}
```

### Error handling

- If `gh` not found, print helpful message and suggest install, then exit
- If `gh` not authenticated, print `gh auth login` instruction
- Parse `gh` stderr for actionable errors (rate limit, not found, etc.)

---

## Asset Matching (`internal/asset/`)

### Platform detection

Use `runtime.GOOS` and `runtime.GOARCH`. Map to common naming conventions:

| `GOOS` | Common names in assets |
|---|---|
| `darwin` | `macos`, `darwin`, `Darwin`, `apple-darwin` |
| `linux` | `linux`, `Linux`, `unknown-linux-gnu`, `unknown-linux-musl` |
| `windows` | `windows`, `Windows`, `win`, `.exe` |

| `GOARCH` | Common names in assets |
|---|---|
| `amd64` | `amd64`, `x86_64`, `x64` |
| `arm64` | `arm64`, `aarch64`, `armv8` |

### Matching algorithm

1. **Filter out non-binaries**: `.sha256`, `.sha512`, `.sig`, `.pem`, `.sbom`, source archives (containing `src` or `source`), `.deb`, `.apk`, `.rpm`, `.msi`, `.pkg`
2. **Hint matching**: if the package was previously installed, tokenize the stored `asset` name and candidate names, strip version-like tokens, and compare structural tokens. If exactly one candidate matches, auto-select.
3. **Score remaining assets** by matching OS and arch keywords
4. **Apply platform priority** from settings to break ties
5. **Prompt**: if multiple plausible matches remain, prompt user to pick from numbered list

### Token-based version stripping

Each token is independently checked: if it starts with a digit (after stripping optional `v`/`V` prefix), it's treated as a version and stripped from matching.

```
ghpm-0.1.6-darwin-amd64.tar.gz  →  ["ghpm", "darwin", "amd64.tar.gz"]
ghpm-0.1.7-darwin-amd64.tar.gz  →  ["ghpm", "darwin", "amd64.tar.gz"]  ← 1:1 match
ghpm-0.1.7-darwin-amd64-extra.tar.gz  →  ["ghpm", "darwin", "amd64", "extra.tar.gz"]  ← no match, prompt
```

### Extraction

#### `ExtractPackage()` — full archive extraction (primary)

Extracts the complete archive into `~/.ghpm/packages/<key>/` preserving directory structure:

| File type | Strategy |
|---|---|
| `.tar.gz` / `.tgz` | Go `archive/tar` + `compress/gzip` — preserve all files, dirs, symlinks |
| `.tar.bz2` | Go `archive/tar` + `compress/bzip2` — same |
| `.tar.xz` | `tar xJf` — shell out to system tar |
| `.zip` | Go `archive/zip` — preserve all files and dirs |
| No extension / raw binary | `chmod +x` and copy directly into package dir |

After extraction, `DiscoverPaths()` walks the directory tree to find bin/lib/share/etc dirs and records them in the manifest.

#### `Extract()` — single-binary extraction (used only for `ghpm upgrade`)

The older extraction method that picks one binary from the archive. Kept for self-upgrade only.

### Path discovery (`internal/asset/discover.go`)

After `ExtractPackage()` extracts the full archive, `DiscoverPaths(pkgDir)` walks the tree:

1. Look for directories named `bin`, `lib`, `share`
2. Under `share/`, look for `man/` subdirectory
3. If no `bin/` dir found, look for directories containing executables (including the package root itself)
4. Record the primary executable name as `binary_name`
5. Skip noise: `__MACOSX`, `.DS_Store`, `.git`

### SHA verification

After downloading, if a `<asset>.sha256` (or `.sha256sum`) file exists among the release assets:

1. Download the `.sha256` file
2. Compute SHA256 of the downloaded asset
3. Compare; error on mismatch
4. Skip verification if `--no-verify` flag or `settings.no_verify` is set
5. If no sidecar file is found, silently skip (no warning)

---

## Shell Env Scripts (`internal/entrypoint/`)

ghpm generates static shell env scripts that users `source` from their shell config. These prepend PATH and other environment variables so installed tools are discoverable. The `internal/entrypoint/` package generates these files to `~/.ghpm/scripts/env.*`.

### Shell detection

On `ghpm init` or after any install/update/uninstall, ghpm detects which shells are available in PATH:

- `sh`/`bash`/`zsh` found → generate `scripts/env.sh`
- `nu` found → generate `scripts/env.nu`
- `pwsh` found → generate `scripts/env.ps1`

Only env scripts for detected shells are generated. Stale env script files for undetected shells are removed.

`ghpm init` prints source hints for **all** detected shells (zsh, bash, nu, pwsh), not just the current shell.

### Regeneration

Env scripts are regenerated from scratch after every:
- `ghpm install`
- `ghpm update`
- `ghpm uninstall`
- `ghpm clean`
- `ghpm init`

This is safe because the process lock prevents concurrent ghpm runs.

### Generated env script examples

**env.sh** (POSIX sh, works for bash/zsh):
```sh
# generated by ghpm — do not edit
GHPM_HOME="/home/user/.ghpm"
GHPM_PKGS="$GHPM_HOME/packages"

# PATH: gh (managed by ghpm)
export PATH="$GHPM_HOME/bin${PATH:+:$PATH}"

# fzf
export PATH="$GHPM_PKGS/fzf:${PATH:+:$PATH}"

# helix
export PATH="$GHPM_PKGS/helix/helix-24.3/helix-24.3/bin:${PATH:+:$PATH}"
export LD_LIBRARY_PATH="$GHPM_PKGS/helix/helix-24.3/helix-24.3/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
export MANPATH="$GHPM_PKGS/helix/helix-24.3/helix-24.3/share/man${MANPATH:+:$MANPATH}"

unset GHPM_HOME GHPM_PKGS
```

**env.nu** (Nushell):
```nu
# generated by ghpm — do not edit
let ghpm_home = "/home/user/.ghpm"
let ghpm_pkgs = $"($ghpm_home)/packages"

# PATH: gh (managed by ghpm)
$env.PATH = ($env.PATH | prepend $"($ghpm_home)/bin")

# fzf
$env.PATH = ($env.PATH | prepend $"($ghpm_pkgs)/fzf")

# helix
$env.PATH = ($env.PATH | prepend $"($ghpm_pkgs)/helix/helix-24.3/helix-24.3/bin")
$env.LD_LIBRARY_PATH = ($env.LD_LIBRARY_PATH? | default [] | prepend $"($ghpm_pkgs)/helix/helix-24.3/helix-24.3/lib")
$env.MANPATH = ($env.MANPATH? | default [] | prepend $"($ghpm_pkgs)/helix/helix-24.3/helix-24.3/share/man")
```

**env.ps1** (PowerShell):
```powershell
# generated by ghpm — do not edit
$ghpm_home = "/home/user/.ghpm"
$ghpm_pkgs = "$ghpm_home\packages"

# PATH: gh (managed by ghpm)
$env:PATH = "$ghpm_home\bin;$env:PATH"

# fzf
$env:PATH = "$ghpm_pkgs\fzf;$env:PATH"

# helix
$env:PATH = "$ghpm_pkgs\helix\helix-24.3\helix-24.3\bin;$env:PATH"
$env:LD_LIBRARY_PATH = "$ghpm_pkgs\helix\helix-24.3\helix-24.3\lib;$env:LD_LIBRARY_PATH"
$env:MANPATH = "$ghpm_pkgs\helix\helix-24.3\helix-24.3\share\man;$env:MANPATH"
```

On Linux, `MANPATH` and `LD_LIBRARY_PATH` are set in **all** shell env scripts (sh, nu, ps1). On non-Linux platforms these variables are omitted.

### User setup

After installing ghpm, users add one line to their shell config:

```sh
# bash
echo 'source ~/.ghpm/scripts/env.sh' >> ~/.bashrc

# zsh
echo 'source ~/.ghpm/scripts/env.sh' >> ~/.zshrc

# nushell
echo 'source ~/.ghpm/scripts/env.nu' >> $nu.config-path

# PowerShell
Add-Content $PROFILE '. ~/.ghpm/scripts/env.ps1'
```

---

## Concurrency & Performance

### Worker pool

All multi-package operations (`install`, `update`, `download`, `uninstall`) use a bounded worker pool:

```go
func Run(ctx context.Context, tasks []Task, workers int) []Result
```

- Default `workers = 5` (from settings)
- Each task runs in a goroutine, results collected via channel
- Context cancellation supported (Ctrl+C)

### Manifest concurrency

- **Orchestrator pattern**: the main goroutine owns all manifest reads and writes
- Workers send results back to the orchestrator via a channel
- The orchestrator serializes manifest updates after workers complete

### Process lock

To prevent concurrent ghpm processes from corrupting the manifest or env scripts:

- `AcquireLock()` acquires an exclusive `flock` on `~/.ghpm/.lock` via `gofrs/flock`
- Cross-platform: `flock(2)` on Linux/macOS, `LockFileEx` on Windows
- Non-blocking try with 3 retries at 1s intervals
- Mutating commands (`install`, `update`, `uninstall`, `clean`, `upgrade`) acquire the lock
- Read-only commands (`list`, `outdated`, `info`, `search`, `doctor`) skip the lock

### GraphQL batched release checking

Checking for updates against many packages is the main performance bottleneck. Instead of one `gh release view` per package, ghpm uses batched GraphQL queries:

- `BatchLatestVersions` groups packages into batches of 50
- Each batch is a single `gh api graphql -f query=... --cache 5m` call
- For floating packages: queries `latestRelease { tagName }`
- For pinned packages: queries `releases(first: 20) { nodes { tagName } }` and filters client-side
- Responses are cached via `gh api --cache` (default 5 minutes)
- **API call reduction**: 1000 packages → ~20 API calls (down from 1000)

Update check flow (two-phase):

1. **Phase 1 — Batch version check**: all packages checked via `BatchLatestVersions`
2. **Phase 2 — Asset detail fetch**: only packages that actually have new versions go through individual `GetReleaseByTag` for asset info

### Rate limit handling

- `ErrRateLimited` sentinel error in `internal/gh/errors.go`
- `run()` in `gh.go` detects `"rate limit"` in stderr from `gh` commands
- `IsRateLimited(err)` helper for callers
- In `outdated` and `update`: rate-limited packages are reported individually, and a summary line prints checked/skipped counts
- **Fail-fast**: no auto-retry or sleep — the user sees which packages were skipped and retries later

---

## Package Name Resolution

Users type `ghpm install fzf`, but we need `github.com/junegunn/fzf`.

### Name constraints

Package names must be simple, filesystem-safe strings with no slashes or spaces. This name becomes the directory name in `~/.ghpm/packages/`. Names like `cli/cli` or `github.com/cli/cli` are rejected — only simple names like `gh`, `fzf`, `ripgrep`.

| Input | Behavior |
|---|---|
| `ghpm install gh` | Look up `gh` in repos → resolve to `github.com/cli/cli` |
| `ghpm install fzf@0.70` | Look up `fzf` in repos → resolve to `github.com/junegunn/fzf`, pin version `0.70` |
| `ghpm install mytool` | No alias match → `gh search repos mytool` → prompt user to pick a repo |
| `ghpm install cli/cli` | **Rejected** — not a valid filename |

### Resolution order

1. **Manifest lookup**: already installed → use stored source
2. **`@version` parsing**: `fzf@0.70` → split name `fzf` + version `0.70`, then resolve `fzf`
3. **Local repo cache**: read all `repos.yaml` files from `~/.ghpm/repos/` subdirectories
4. **GitHub search fallback**: use `gh search repos <name>` to find candidates

### Deduplication

Before installing, scan the manifest for any existing entry whose source matches the resolved URI. If found, warn that the package is already installed under a different name and skip.

### Remote repos format

Each repo source (configurable via `repo_sources` in settings) must have a `repos.yaml` at its root:

```yaml
repos:
  fzf: github.com/junegunn/fzf
  ripgrep: github.com/BurntSushi/ripgrep
  rg: github.com/BurntSushi/ripgrep
  bat: github.com/sharkdp/bat
  delta: github.com/dandavison/delta
  lazygit: github.com/jesseduffield/lazygit
```

Each source's file is cached at `~/.ghpm/repos/github.com/<owner>/<repo>/repos.yaml`. On load, ghpm walks the entire `~/.ghpm/repos/` tree and merges all `repos.yaml` files it finds — later files win on key conflict. Only `ghpm update` fetches fresh copies from remote.

### `@version` syntax

```
ghpm install fzf              # latest, dir → packages/fzf/
ghpm install fzf@14           # latest 14.x, dir → packages/fzf@14/
ghpm install fzf@14.1         # latest 14.1.x, dir → packages/fzf@14.1/
ghpm install fzf@14.1.0       # exact, dir → packages/fzf@14.1.0/ (never updates)
ghpm update fzf@14 ripgrep    # updates fzf@14 within major, and ripgrep to latest
ghpm uninstall fzf@14         # removes only the major-pinned copy
```

- **Floating** (`fzf`): `ghpm update` always fetches the newest release.
- **Major pin** (`fzf@14`): `ghpm update` finds the latest `14.x.x`.
- **Minor pin** (`fzf@14.1`): `ghpm update` finds the latest `14.1.x`.
- **Exact pin** (`fzf@14.1.0`): `ghpm outdated` and `ghpm update` skip it entirely.

---

## CLI Commands

Using cobra, with global flags and subcommands.

### Root command

```
ghpm [--dry-run] [--no-verify] [--version] [--help] <command>
```

| Flag | Scope | Description |
|---|---|---|
| `--dry-run` | global, persistent | Print what would be done without executing |
| `--no-verify` | global, persistent | Skip SHA256 verification |
| `--version` | root only | Print ghpm version |
| `--help` | all commands | Print help |

### Commands

All commands that accept package names accept **multiple names** and process them in parallel (up to `settings.num_parallel`).

#### `ghpm clean [--all]`

1. Default: scan `~/.ghpm/releases/`, compare against manifest, remove assets for versions not currently installed
2. Scan `~/.ghpm/packages/`, remove dirs not matching any manifest key
3. Regenerate env scripts
4. `--all`: remove entire `~/.ghpm/releases/` contents + orphaned packages + regenerate env scripts
5. Prompt before deleting

#### `ghpm doctor`

Diagnostic command — checks system health:

1. `gh` installed? Authenticated? (`gh auth status`)
2. Env script files exist for detected shells?
3. Env script sourced in user's shell rc file(s)?
4. Manifest file valid JSON?
5. Settings file valid (if present)?
6. All installed packages present on disk?
7. Disk usage of `~/.ghpm/releases/` cache
8. Print summary with PASS/FAIL/WARN per check

#### `ghpm download <names> [--path <dir>]`

1. Like `install` but skip extraction
2. Download to cache by default, or `--path` if given
3. Prompt before downloading

#### `ghpm info <names>`

1. Resolve name → source
2. Fetch release list (or specific release)
3. Print: source repo, available versions (last 10), asset list for selected version

#### `ghpm init [--shell <shell>]`

1. Detect available shells in PATH
2. Generate env script files for detected shells (or only `--shell` if specified)
3. Print source instructions for **all** detected shells (zsh, bash, nu, pwsh)
4. Supported shells: `sh`/`bash`/`zsh`, `nu`, `pwsh`

#### `ghpm install <names>`

1. Parse each name → resolve source + optional version
2. Fetch release info for each (parallel)
3. Run asset matcher for each (parallel)
4. Prompt with table showing what will be installed
5. Download + verify SHA + extract full archive to `~/.ghpm/packages/<key>/` (parallel)
6. Discover paths in extracted tree
7. Update manifest (store version, asset, paths, binary_name)
8. Regenerate env scripts

#### `ghpm list`

1. Load manifest
2. Print table: `NAME  PIN  VERSION  ASSET  REPO`

#### `ghpm outdated`

1. Load manifest
2. Batch-check all packages via GraphQL
3. Print table of packages with newer versions available: `NAME  PIN  INSTALLED  AVAILABLE  SOURCE`

#### `ghpm uninstall <names>`

1. Load manifest, find entries
2. Prompt with table
3. Remove `~/.ghpm/packages/<key>/` directory
4. Remove entry from manifest
5. Regenerate env scripts
6. Leave cached release assets (cleaned by `ghpm clean`)

#### `ghpm update [names]`

1. If no names given, update all floating and major/minor-pinned packages (exact pins are skipped)
2. If names given, update only those (exact pins warn and skip)
3. Batch-check versions via GraphQL
4. For packages with new versions: use stored `asset` for token-based matching, fetch asset details, download, extract
5. Remove old `packages/<key>/`, re-extract, rediscover paths
6. Prompt before updating
7. Regenerate env scripts
8. Refreshes all configured alias repos before checking for updates

#### `ghpm upgrade`

1. Fetch latest release of ghpm from `github.com/meop/ghpm`
2. Compare with current version
3. If newer, download + extract single binary + replace running binary in-place
4. Also check and upgrade the managed `gh` copy at `~/.ghpm/bin/gh` by fetching the latest release of `cli/cli`
5. If both already latest, print current versions and exit

---

## Build & Release

### Makefile targets

- `build` — build for current platform
- `build-all` — cross-compile for `{linux,darwin,windows}/{amd64,arm64}`
- `test` — run tests
- `lint` — run `golangci-lint`
- `install` — build and install to `$GOPATH/bin`

### GoReleaser

Use [goreleaser/goreleaser](https://github.com/goreleaser/goreleaser) to automate cross-compilation and release publishing.

### Build pipeline

`go build -ldflags "-X main.version=<tag>"`

---

## Resolved Decisions

| Topic | Decision |
|---|---|
| `@version` syntax | Homebrew-style `name@version`. Three pin levels: major (`@14`), minor (`@14.1`), exact (`@14.1.0`). Exact pins never update. |
| `gh` auth | Public repos need no auth. ghpm warns if `gh` not authenticated for private repos. |
| Asset selection | Store chosen filename in manifest (`asset` field). Token-based structural matching on update: strip version-like tokens, compare remaining tokens. Auto-select if exactly one candidate matches. |
| Batch release checks | GraphQL batch queries via `gh api graphql` with `--cache`. Groups of 50 repos per call. Two-phase update: batch version check → individual asset fetch for changed packages only. |
| Binary naming | `@` separator for versioned packages: `fzf@0.70.0` (not `-`). Consistent with manifest key syntax, avoids ambiguity with hyphenated package names. |
| Entrypoint generation | Static env scripts in `~/.ghpm/scripts/env.*` regenerated by ghpm after every mutation. Shell-aware: only generates for shells detected in PATH. |
| Extraction model | All archives extracted as-is into `packages/<key>/`. Path discovery finds bin/lib/share dirs. `~/.ghpm/bin/` holds `ghpm` and `gh` (not in manifest, managed by `ghpm upgrade`). |
| Manifest concurrency | Orchestrator goroutine owns all reads/writes. Workers communicate via channels. |
| Manifest format | JSON — stdlib only, no comments needed (machine-managed). Keyed by simple package name. |
| Multiple binaries in archive | Full archive preserved. All executables discoverable via path discovery. |
| Package names | Must be simple filenames (no slashes). No `owner/repo` shorthand. Resolution: manifest → builtins → repos.yaml → `gh search repos`. |
| Parallelism | 5 workers default, configurable via `settings.num_parallel`. |
| PATH management | Users add `~/.ghpm/bin/` to PATH and `source` the generated env script (`~/.ghpm/scripts/env.*`) from their shell config. `ghpm init` prints source instructions for all detected shells. Env scripts always prepend `~/.ghpm/bin/` to PATH (for `ghpm` and `gh`). |
| Platform priorities | Windows: MSVC > GNU; Linux: GNU > Musl. Configurable in `settings.json`. |
| Process locking | Exclusive `flock` on `~/.ghpm/.lock` via `gofrs/flock`. Acquired by all mutating commands. Read-only commands skip the lock. |
| Rate limiting | Detect `"rate limit"` in `gh` stderr, return `ErrRateLimited`. Fail-fast: report skipped packages and counts. No auto-retry. |
| Repo sources | One or more configured via `repo_sources`. Cached locally, refreshed only during `ghpm update`. |
| SHA verification | On by default, skip with `--no-verify` or `settings.no_verify`. |
| Shell detection | `exec.LookPath` for sh/bash/zsh, nu, pwsh. Only generate env scripts for detected shells. `ghpm init` prints source hints for all detected shells. |
| Version normalization | Strip all leading non-digit characters from GitHub tags. Handles `v1.2.3`, `bun-v1.3.13`, `release-0.1.0`. |
