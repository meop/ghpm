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
│   ├── gh/           # gh CLI wrapper, GraphQL batch queries, rate limit detection
│   ├── parallel/     # worker pool orchestration
│   ├── shim/         # shim creation/removal (symlink on Unix, .cmd on Windows)
│   └── store/        # path helpers for ~/.ghpm/ subdirectories
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
│   ├── ghpm                    # ghpm itself (managed by ghpm upgrade)
│   ├── fzf -> ../extracts/fzf/0.58.0/fzf          # shim: symlink (Unix)
│   ├── fzf@0.57 -> ../extracts/fzf@0.57/0.57.3/fzf # shim: versioned
│   └── nvim.cmd                # shim: .cmd wrapper (Windows)
├── extracts/                   # installed packages (full archive extracts)
│   ├── fzf/
│   │   └── 0.58.0/             # versioned extract dir
│   │       └── fzf             # single binary
│   ├── helix/
│   │   └── 24.3/
│   │       └── helix-24.3-x86_64-linux/
│   │           └── helix-24.3/
│   │               ├── bin/hx
│   │               ├── lib/
│   │               ├── share/
│   │               └── runtime/
│   └── fzf@0.57/
│       └── 0.57.3/
│           └── fzf
├── manifest.json               # tracked packages (gh is NOT in the manifest)
├── releases/                   # cached release assets
│   └── github.com/<owner>/<repo>/<version>/<asset>
├── repos/                      # cached repo files per source
│   └── github.com/<owner>/<repo>/repos.yaml
└── settings.json               # user preferences (optional, not created by default)
```

### Manifest (`manifest.json`)

Plain JSON, fully managed by ghpm:

```json
{
  "repos": {
    "bun": "github.com/oven-sh/bun",
    "fzf": "github.com/junegunn/fzf"
  },
  "extracts": {
    "bun": {
      "pin": "latest",
      "version": "1.3.13",
      "asset_name": "bun-linux-x64.zip",
      "bin_dir": "",
      "bin_name": "bun"
    },
    "helix": {
      "pin": "latest",
      "version": "24.3",
      "asset_name": "helix-24.3-x86_64-linux.tar.xz",
      "bin_dir": "helix-24.3-x86_64-linux/helix-24.3/bin",
      "bin_name": "hx"
    },
    "fzf@0.57": {
      "pin": "minor",
      "version": "0.57.3",
      "asset_name": "fzf-0.57.3-linux_amd64.tar.gz",
      "bin_dir": "",
      "bin_name": "fzf"
    }
  }
}
```

Fields:

- **`pin`**: `"latest"`, `"major"`, `"minor"`, or `"fixed"`. Floating packages track the newest release; pinned packages update within their constraint; exact pins never update.
- **`version`**: installed version, normalized by stripping all leading non-digit characters from the GitHub tag (e.g., `"bun-v1.3.13"` → `"1.3.13"`, `"v0.71.0"` → `"0.71.0"`).
- **`asset_name`**: the exact asset filename chosen during install. On `update`, ghpm tokenizes both the stored and candidate asset names, strips version-like tokens, and matches on the remaining structural tokens. If exactly one candidate matches, it is auto-selected without prompting.
- **`bin_dir`**: subdirectory within the extract dir where the binary lives (relative to `extracts/<key>/<version>/`). Empty string means the binary is at the extract root.
- **`bin_name`**: the primary executable name (without `.exe`). Used to construct the shim target path.

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
- `EnsureDirs() error` — create `~/.ghpm/{bin,extracts,releases,repos,scripts}` if missing
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
2. **Hint matching**: if the package was previously installed, tokenize the stored `asset_name` and candidate names, strip version-like tokens, and compare structural tokens. If exactly one candidate matches, auto-select.
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

#### `ExtractPackage()` — full archive extraction

Extracts the complete archive into `~/.ghpm/extracts/<key>/<version>/` preserving directory structure:

| File type | Strategy |
|---|---|
| `.tar.gz` / `.tgz` | Go `archive/tar` + `compress/gzip` — preserve all files, dirs, symlinks |
| `.tar.bz2` | Go `archive/tar` + `compress/bzip2` — same |
| `.tar.xz` | `tar xJf` — shell out to system tar |
| `.zip` | Go `archive/zip` — preserve all files and dirs |
| No extension / raw binary | `chmod +x` and copy directly into extract dir |

After extraction, `DiscoverPaths()` finds the binary by name.

### Path discovery (`internal/asset/discover.go`)

`DiscoverPaths(pkgDir, name string)` finds the binary using name-based lookup with magic-byte verification:

1. Look for a file named exactly `name` (or `name.exe` on Windows) in: root, `bin/`, each top-level subdir, and `<subdir>/bin/`
2. Verify the file is a real binary via magic bytes: ELF (`\x7fELF`) on Linux, Mach-O variants on macOS, file existence on Windows
3. Returns `(binDir, binaryName)` — `binDir` is the subdirectory path relative to `pkgDir`, empty string if binary is at root
4. Calls `ensureExecutable` to set the execute bit if missing

This avoids false positives from scripts or data files that happen to be executable.

### Verification

After downloading, ghpm runs `gh release verify-asset` (Sigstore attestation) against the downloaded file:

1. Run `gh release verify-asset <tag> <asset-path> -R owner/repo`
2. If attestation is not found or not published, skip silently (returns false, nil)
3. If verification fails for any other reason, return an error
4. Skip entirely if `--no-verify` flag or `settings.no_verify` is set

---

## Shims (`internal/shim/`)

Each installed binary gets a shim in `~/.ghpm/bin/` pointing at the real binary inside its versioned extract dir. Adding `~/.ghpm/bin` to PATH is all the shell setup required.

### Shim types

- **Unix (Linux/macOS)**: a symlink — `~/.ghpm/bin/<key>` → `~/.ghpm/extracts/<key>/<version>/<binDir>/<binaryName>`
- **Windows**: a `.cmd` wrapper — `~/.ghpm/bin/<key>.cmd` containing `@"<absolute-path-to-binary.exe>" %*`

Symlinks are resolved by the kernel before exec, so the real binary sees its own location via `/proc/self/exe` (Linux) or `_NSGetExecutablePath` (macOS). Portable tools that locate runtime data relative to their own path (e.g., `nvim` finding `../share/nvim/runtime`) work correctly.

### Shim lifecycle

- **Install**: `shim.Create(key, binaryName, pkgDir, binSubdir)` — creates or overwrites the shim
- **Update**: same call after extracting the new version — shim is atomically replaced to point at the new path
- **Uninstall**: `shim.Remove(key)` — removes the shim file
- **Clean**: `cleanOrphanedShims` scans `~/.ghpm/bin/` and removes any file not backed by a manifest entry

### Multiple versions alongside each other

Each manifest key gets its own shim: `fzf`, `fzf@0`, `fzf@1.2.3` → `~/.ghpm/bin/fzf`, `~/.ghpm/bin/fzf@0`, `~/.ghpm/bin/fzf@1.2.3`. They coexist without conflict.

### Shell setup

Users add one line to their shell config:

```sh
# bash/zsh (~/.bashrc or ~/.zshrc)
eval "$(ghpm init)"

# nushell (~/.config/nushell/env.nu)
$env.PATH = ($env.PATH | prepend ($env.HOME + "/.ghpm/bin") | uniq)

# PowerShell ($PROFILE)
Invoke-Expression (ghpm init pwsh)
```

`ghpm init [shell]` outputs the appropriate static PATH snippet. Supported shell arguments: `nu`/`nushell`, `pwsh`/`powershell`; anything else (including no argument) outputs POSIX sh.

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

To prevent concurrent ghpm processes from corrupting the manifest:

- `AcquireLock()` acquires an exclusive `flock` on `~/.ghpm/.lock` via `gofrs/flock`
- Cross-platform: `flock(2)` on Linux/macOS, `LockFileEx` on Windows
- Non-blocking try with 3 retries at 1s intervals
- Mutating commands (`install`, `update`, `uninstall`, `clean`, `upgrade`) acquire the lock
- Read-only commands (`list`, `outdated`, `show`, `search`, `doctor`) skip the lock

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

Package names must be simple, filesystem-safe strings with no slashes or spaces. This name becomes part of the directory path in `~/.ghpm/extracts/`. Names like `cli/cli` or `github.com/cli/cli` are rejected — only simple names like `gh`, `fzf`, `ripgrep`.

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
ghpm install fzf              # latest, extracts → extracts/fzf/<version>/
ghpm install fzf@14           # latest 14.x, extracts → extracts/fzf@14/<version>/
ghpm install fzf@14.1         # latest 14.1.x, extracts → extracts/fzf@14.1/<version>/
ghpm install fzf@14.1.0       # exact, extracts → extracts/fzf@14.1.0/<version>/ (never updates)
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
2. Scan `~/.ghpm/extracts/`, remove stale version subdirs and dirs not matching any manifest key
3. Scan `~/.ghpm/bin/`, remove shims not backed by a manifest entry
4. `--all`: remove entire `~/.ghpm/releases/` contents, then run orphan cleanup
5. Prompt before deleting each category

#### `ghpm doctor`

Diagnostic command — checks system health:

1. `gh` installed? Authenticated? (`gh auth status`)
2. Manifest file valid JSON?
3. Settings file valid (if present)?
4. All installed packages present on disk (extract dir exists)?
5. Shim count in `~/.ghpm/bin/`
6. Disk usage of `~/.ghpm/releases/` cache
7. Print summary with PASS/FAIL/WARN per check

#### `ghpm download <names> [--path <dir>]`

1. Like `install` but skip extraction
2. Download to cache by default, or `--path` if given
3. Prompt before downloading

#### `ghpm show <names>`

1. Resolve name → source
2. Fetch release list (or specific release)
3. Print: source repo, available versions (last 10), asset list for selected version

#### `ghpm init [shell]`

Output a static PATH snippet for `~/.ghpm/bin` suitable for eval in a shell config file. Supported shell arguments: `nu`/`nushell`, `pwsh`/`powershell`; anything else (or no argument) outputs POSIX sh.

#### `ghpm install <names>`

1. Parse each name → resolve source + optional version
2. Fetch release info for each (parallel)
3. Run asset matcher for each (parallel)
4. Prompt with table showing what will be installed
5. Pre-remove existing extract dir (clean slate for `--force` reinstall)
6. Download + verify SHA + extract full archive to `~/.ghpm/extracts/<key>/<version>/` (parallel)
7. On extraction failure, remove partial extract dir immediately
8. Discover binary via name-based lookup + magic bytes
9. If binary not found, leave extract dir for `ghpm clean` (hadErrors = true)
10. Create shim in `~/.ghpm/bin/<key>`
11. Update manifest

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
3. Remove `~/.ghpm/extracts/<key>/<version>/` directory; remove base dir if now empty
4. Remove shim `~/.ghpm/bin/<key>`
5. Remove entry from manifest (and from `repos` if no other version of same base name remains)
6. Leave cached release assets (cleaned by `ghpm clean`)

#### `ghpm update [names]`

1. If no names given, update all floating and major/minor-pinned packages (exact pins are skipped)
2. If names given, update only those (exact pins warn and skip)
3. Batch-check versions via GraphQL
4. For packages with new versions: use stored `asset_name` for token-based matching, fetch asset details
5. Pre-remove new version extract dir (clean re-extract if retrying)
6. Download + verify SHA + extract to `~/.ghpm/extracts/<key>/<newVersion>/`
7. On extraction failure, remove partial dir immediately; old version dir untouched
8. Discover binary; on failure leave new dir for `ghpm clean`, old version dir untouched
9. On success: remove old version dir, update shim, update manifest
10. Prompt before updating
11. Refreshes all configured alias repos before checking for updates

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
| Asset selection | Store chosen filename in manifest (`asset_name` field). Token-based structural matching on update: strip version-like tokens, compare remaining tokens. Auto-select if exactly one candidate matches. |
| Batch release checks | GraphQL batch queries via `gh api graphql` with `--cache`. Groups of 50 repos per call. Two-phase update: batch version check → individual asset fetch for changed packages only. |
| Binary naming | `@` separator for versioned packages: `fzf@0.70` (not `-`). Consistent with manifest key syntax, avoids ambiguity with hyphenated package names. |
| Extraction model | Full archives extracted to `extracts/<key>/<version>/`. Versioned dirs enable atomic updates: extract new → verify binary → remove old. Failed extracts leave the partial dir for `ghpm clean`. |
| Manifest concurrency | Orchestrator goroutine owns all reads/writes. Workers communicate via channels. |
| Manifest format | JSON — stdlib only, no comments needed (machine-managed). Keyed by package name (with optional `@version` suffix). |
| Multiple versions alongside | Each manifest key (`fzf`, `fzf@0`, `fzf@1.2.3`) gets its own shim. They coexist in `~/.ghpm/bin/` without conflict. |
| PATH management | Single `~/.ghpm/bin/` directory in PATH. Each installed binary gets a shim there (symlink on Unix, `.cmd` on Windows). `ghpm init [shell]` outputs the static PATH snippet for the user's shell config. No reload or regeneration needed after installs. |
| Parallelism | 5 workers default, configurable via `settings.num_parallel`. |
| Path discovery | Name-based lookup: search `pkgDir` for a file named exactly `binaryName` (or `binaryName.exe` on Windows), verified via magic bytes (ELF/Mach-O). Avoids false positives from scripts with execute bits. |
| Platform priorities | Windows: MSVC > GNU; Linux: GNU > Musl. Configurable in `settings.json`. |
| Process locking | Exclusive `flock` on `~/.ghpm/.lock` via `gofrs/flock`. Acquired by all mutating commands. Read-only commands skip the lock. |
| Rate limiting | Detect `"rate limit"` in `gh` stderr, return `ErrRateLimited`. Fail-fast: report skipped packages and counts. No auto-retry. |
| Repo sources | One or more configured via `repo_sources`. Cached locally, refreshed only during `ghpm update`. |
| Verification | `gh release verify-asset` (Sigstore attestation). Silently skipped if no attestation exists. Skip entirely with `--no-verify` or `settings.no_verify`. |
| Shim design | Symlinks on Linux/macOS: OS resolves them before exec, so the binary sees its real path and relative data dirs work. `.cmd` wrappers on Windows: call the binary with its absolute path, no `cmd.exe` Ctrl+C issues since the console is inherited directly. |
| Version normalization | Strip all leading non-digit characters from GitHub tags. Handles `v1.2.3`, `bun-v1.3.13`, `release-0.1.0`. |
