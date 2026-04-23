# ghpm Design Document

## Overview

ghpm is a Go CLI package manager that installs binaries from GitHub Releases, using `gh` as its primary interface to GitHub.

**Repository**: `github.com/meop/ghpm`
**Config repo**: `github.com/meop/ghpm-config` (remote aliases, platform priorities)

---

## Phase 1: Project Scaffolding

### 1.1 Go module and directory layout

Initialize a Go module and create the directory structure:

```
ghpm/
├── cmd/
│   └── ghpm/
│       └── main.go
├── internal/
│   ├── cli/          # cobra command definitions
│   ├── config/       # manifest, settings, alias resolution
│   ├── gh/           # gh CLI wrapper
│   ├── asset/        # asset matching, selection, extraction
│   ├── store/        # download cache, bin management
│   ├── selfupdate/   # self-upgrade logic
│   └── parallel/     # worker pool orchestration
├── install.sh
├── install.ps1
├── go.mod
├── go.sum
├── DESIGN.txt
└── PLAN.md
```

### 1.2 Dependencies

| Dependency | Purpose |
|---|---|
| `github.com/spf13/cobra` | CLI framework (commands, flags, help, completion) |
| `github.com/fatih/color` | Terminal color output |
| `gopkg.in/yaml.v3` | Parse remote aliases.yaml from ghpm-config |
| `encoding/json` | Manifest, settings — stdlib only |

No heavy GitHub SDK — all GitHub interaction goes through `gh` CLI invoked via `os/exec`.

---

## Phase 2: Config & Manifest

### 2.1 Directory structure (`~/.ghpm/`)

```
~/.ghpm/
├── bin/                        # installed binaries (added to PATH)
├── release/                    # cached release assets
│   └── github.com/
│       └── <owner>/
│           └── <repo>/
│               └── <version>/
│                   └── <asset>
├── manifest.json               # tracked packages
└── settings.json               # user preferences (optional)
```

### 2.2 Manifest format (`manifest.json`)

Plain JSON, fully managed by ghpm:

```json
{
  "packages": {
    "fzf": {
      "source": "github.com/junegunn/fzf",
      "version": "0.56.0",
      "versioned": false,
      "asset_pattern": "fzf-0.56.0-linux_amd64.tar.gz",
      "installed_at": "2026-04-23T10:00:00Z"
    },
    "fzf@0.70.0": {
      "source": "github.com/junegunn/fzf",
      "version": "0.70.0",
      "versioned": true,
      "asset_pattern": "fzf-0.70.0-linux_amd64.tar.gz",
      "installed_at": "2026-04-23T11:00:00Z"
    }
  }
}
```

Key fields:

- **`versioned`**: `true` means installed with `@version` syntax; binary gets version suffix (`fzf@0.70.0`). `false` means latest-tracking; binary gets plain name (`fzf`).
- **`asset_pattern`**: the exact asset filename chosen during install. On `update`, ghpm uses this to prioritize the same naming pattern when matching assets in the latest release. This avoids re-prompting the user for the same choice on every update.

### 2.3 Settings (`settings.json`, optional)

Defaults are hardcoded. User can override by creating `~/.ghpm/settings.json`:

```json
{
  "parallelism": 5,
  "platform_priority": {
    "linux": ["gnu", "musl"],
    "windows": ["msvc", "gnu"]
  },
  "no_verify": false
}
```

- **`parallelism`**: max concurrent download/extract operations (default: 5)
- **`platform_priority`**: when multiple assets match OS+arch, prefer toolchains in this order. Default: Windows → MSVC over GNU; Linux → GNU over Musl.
- **`no_verify`**: skip SHA verification globally (default: false)

### 2.4 Config module (`internal/config/`)

- `LoadManifest() (*Manifest, error)` — read manifest, create if missing
- `SaveManifest(m *Manifest) error` — atomic write (write to temp, rename)
- `LoadSettings() (*Settings, error)` — load settings with hardcoded defaults for missing file
- `EnsureDirs() error` — create `~/.ghpm/{bin,release}` if missing
- Define `PackageEntry`, `Manifest`, `Settings` structs

---

## Phase 3: gh CLI Wrapper (`internal/gh/`)

All GitHub interaction goes through `gh`.

### 3.1 Core functions

| Function | `gh` command | Description |
|---|---|---|
| `CheckInstalled() error` | `which gh` | Verify `gh` is available and authenticated |
| `ListReleases(owner, repo string) ([]Release, error)` | `gh release list -R owner/repo --json tagName,isLatest` | Fetch all releases |
| `GetLatestRelease(owner, repo string) (Release, error)` | `gh release view -R owner/repo --json tagName,assets` | Get latest release with assets |
| `GetReleaseByTag(owner, repo, tag string) (Release, error)` | `gh release view tag -R owner/repo --json tagName,assets` | Get specific release |
| `DownloadAsset(owner, repo, tag, pattern string, dest string) error` | `gh release download tag -R owner/repo -p pattern -D dest` | Download matching asset(s) |

### 3.2 Data types

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

### 3.3 Error handling

- If `gh` not found, print helpful message and suggest install, then exit
- If `gh` not authenticated, print `gh auth login` instruction
- Parse `gh` stderr for actionable errors (rate limit, not found, etc.)

---

## Phase 4: Asset Matching (`internal/asset/`)

This is the hardest problem — mapping a release's assets to the correct one for the user's platform.

### 4.1 Platform detection

Use `runtime.GOOS` and `runtime.GOARCH`. Map to common naming conventions:

| `GOOS` | Common names in assets |
|---|---|
| `linux` | `linux`, `Linux`, `unknown-linux-gnu`, `unknown-linux-musl` |
| `darwin` | `macos`, `darwin`, `Darwin`, `apple-darwin` |
| `windows` | `windows`, `Windows`, `win`, `.exe` |

| `GOARCH` | Common names in assets |
|---|---|
| `amd64` | `amd64`, `x86_64`, `x64`, `64bit` |
| `arm64` | `arm64`, `aarch64`, `armv8` |

### 4.2 Matching algorithm

1. **Filter out non-binaries**: `.sha256`, `.sha512`, `.sig`, `.pem`, `.sbom`, source archives (containing `src` or `source`), `.deb`, `.apk`, `.rpm`, `.msi`, `.pkg`
2. **Score each remaining asset** by matching OS and arch keywords
3. **Apply platform priority** from settings (e.g., on Linux prefer `gnu` over `musl`; on Windows prefer `msvc` over `gnu`) to break ties
4. **Check `asset_pattern`** from manifest: if the package was previously installed, prioritize assets whose naming matches the stored pattern (same prefix, same suffix style)
5. **Selection**:
   - **Exactly one** high-confidence match → auto-select
   - **Multiple** plausible matches → prompt user to pick from numbered list
   - **Zero** matches → print that no compatible asset was found, list all available assets
6. **Store chosen asset filename** in `manifest.asset_pattern` for future update operations

### 4.3 Extraction

Determine how to extract the binary from the downloaded asset:

| File type | Strategy |
|---|---|
| `.tar.gz` / `.tgz` | `tar xzf` — find binary inside |
| `.tar.bz2` | `tar xjf` — same |
| `.tar.xz` | `tar xJf` — same |
| `.zip` | `unzip` — same |
| No extension / raw binary | `chmod +x` and copy directly |

When extracting from archives, prefer files that are executable and match the package name. If multiple candidates, pick the largest binary or prompt. The goal is always to install exactly **one** binary per package.

### 4.4 SHA verification

After downloading, if a `<asset>.sha256` (or `.sha256sum`) file exists among the release assets:

1. Download the `.sha256` file
2. Compute SHA256 of the downloaded asset
3. Compare; error on mismatch
4. Skip verification if `--no-verify` flag or `settings.no_verify` is set

---

## Phase 5: Parallel Execution (`internal/parallel/`)

### 5.1 Worker pool

All multi-package operations (`install`, `update`, `download`, `uninstall`) use a bounded worker pool:

```go
func Run(ctx context.Context, tasks []Task, workers int) []Result
```

- Default `workers = 5` (from settings)
- Each task runs in a goroutine, results collected via channel
- Context cancellation supported (Ctrl+C)

### 5.2 Manifest concurrency

The manifest is a single shared file. Concurrency strategy:

- **Orchestrator pattern**: the main goroutine owns all manifest reads and writes
- Workers send results back to the orchestrator via a channel
- Orchestrator serializes manifest updates after each worker completes
- `sync.Mutex` around `SaveManifest()` for safety
- This avoids file-lock complexity since ghpm is a single short-lived process

---

## Phase 6: Package Name Resolution (`internal/config/`)

Users type `ghpm install fzf`, but we need `github.com/junegunn/fzf`.

### 6.1 Resolution order

1. **Manifest lookup**: already installed → use stored `source`
2. **`@version` parsing**: `fzf@0.70` → split name `fzf` + version `0.70`, then resolve `fzf`
3. **Remote aliases**: fetch `aliases.yaml` from `github.com/meop/ghpm-config` (cached locally for the session). This is the primary alias source.
4. **Shorthand syntax**: `owner/repo` → `github.com/owner/repo`
5. **Full URI**: `github.com/owner/repo` → use directly
6. **GitHub search fallback**: use `gh search repos <name>` to find candidates. Prompt the user to confirm which repo they meant.

### 6.2 Remote aliases format

Fetched from `github.com/meop/ghpm-config/cfg/aliases.yaml`:

```yaml
aliases:
  fzf: github.com/junegunn/fzf
  ripgrep: github.com/BurntSushi/ripgrep
  rg: github.com/BurntSushi/ripgrep
  bat: github.com/sharkdp/bat
  delta: github.com/dandavison/delta
  lazygit: github.com/jesseduffield/lazygit
```

Fetched once per session (or on demand), cached in memory. Not written to disk — always fresh from remote so updates to the config repo propagate immediately.

### 6.3 `@version` syntax

Homebrew-style version pinning:

```
ghpm install fzf@0.70        # installs fzf version 0.70.x, binary → fzf@0.70.0
ghpm install fzf              # installs latest fzf, binary → fzf
ghpm install fzf ripgrep bat  # installs latest of all three in parallel
ghpm update fzf@0.70 ripgrep  # updates only fzf@0.70 and ripgrep
ghpm uninstall fzf@0.70       # removes the versioned copy only
```

The manifest key includes the version for versioned installs: `fzf@0.70.0`.

---

## Phase 7: CLI Commands (`internal/cli/`)

Using cobra, define a root command with global flags and subcommands.

### 7.1 Root command

```
ghpm [--dry-run] [--no-verify] [--version] [--help] <command>
```

| Flag | Scope | Description |
|---|---|---|
| `--dry-run` | global, persistent | Print what would be done without executing |
| `--no-verify` | global, persistent | Skip SHA256 verification |
| `--version` | root only | Print ghpm version |
| `--help` | all commands | Print help |

### 7.2 Commands

All commands that accept package names accept **multiple names** and process them in parallel (up to `settings.parallelism`).

#### `ghpm install <names>`

Examples: `ghpm install fzf`, `ghpm install fzf@0.70 ripgrep bat`

1. Parse each name → resolve source + optional version (via `@version` syntax)
2. Fetch release info (specific tag or latest) for each
3. Run asset matcher for each (parallel)
4. Prompt: `Install fzf 0.56.0? [y/N]` (per package, or batch)
5. Download + verify SHA + extract to `~/.ghpm/bin/`
6. Binary naming: unversioned → `fzf`, versioned → `fzf@0.70.0`
7. Update manifest (store `asset_pattern`)

#### `ghpm list`

1. Load manifest
2. Print table: `NAME  VERSION  VERSIONED  SOURCE`

#### `ghpm info <names>`

Examples: `ghpm info fzf`, `ghpm info fzf@0.70`

1. Resolve name → source
2. Fetch release list (or specific release)
3. Print: source repo, available versions (last 10), asset list for selected version
4. Useful for debugging asset matching and seeing what's available

#### `ghpm download <names> [--path <dir>]`

Examples: `ghpm download fzf`, `ghpm download fzf@0.70 --path /tmp`

1. Like `install` but skip extraction
2. Download to cache by default, or `--path` if given
3. Prompt before downloading

#### `ghpm outdated`

1. Load manifest
2. For each package where `versioned == false`:
   - Fetch latest release tag from GitHub
   - Compare with installed version
   - If newer, add to outdated list
3. Print table: `NAME  INSTALLED  LATEST`
4. Versioned packages (`fzf@0.70`) are never shown as outdated — they are pinned

#### `ghpm update [names]`

Examples: `ghpm update`, `ghpm update fzf ripgrep`

1. If no names given, update all unversioned (`versioned == false`) packages
2. If names given, update only those (must be unversioned)
3. For each: download latest, extract, replace binary, update manifest
4. Uses stored `asset_pattern` to prefer the same asset naming
5. Prompt before each update (or batch prompt)

#### `ghpm uninstall <names>`

Examples: `ghpm uninstall fzf`, `ghpm uninstall fzf@0.70 ripgrep`

1. Load manifest, find entries
2. Prompt: `Uninstall fzf 0.56.0? [y/N]`
3. Remove binary from `~/.ghpm/bin/`
4. Remove entry from manifest
5. Leave cached release assets (cleaned by `ghpm clean`)

#### `ghpm clean [--all]`

1. Default: scan `~/.ghpm/release/`, compare against manifest, remove assets for versions not currently installed
2. `--all`: remove entire `~/.ghpm/release/` contents
3. Prompt before deleting

#### `ghpm upgrade`

1. Fetch latest release of ghpm from `github.com/meop/ghpm`
2. Compare with current version
3. If newer, download + extract + replace running binary in-place
4. If already latest, print current version and exit

#### `ghpm doctor`

Diagnostic command — checks system health:

1. `gh` installed? Authenticated? (`gh auth status`)
2. `~/.ghpm/bin` in PATH?
3. Manifest file valid JSON?
4. Settings file valid (if present)?
5. Disk usage of `~/.ghpm/release/` cache
6. Print summary with PASS/FAIL per check

---

## Phase 8: Confirmation Prompts & Dry-Run

### 8.1 Confirmation

Every mutating operation (`install`, `update`, `uninstall`, `clean`, `upgrade`) must prompt `[y/N]` before proceeding. Use `bufio.NewReader(os.Stdin).ReadString('\n')` for input.

For multi-package operations, offer a batch prompt: `Install fzf 0.56.0, ripgrep 14.1.0, bat 0.24.0? [y/N]`

In `--dry-run` mode, skip prompts and just print what would happen.

### 8.2 Dry-run output

Print the `gh` commands that would be run, URLs that would be downloaded, file paths that would be written, and binaries that would be created or replaced.

---

## Phase 9: Self-Upgrade

1. `ghpm upgrade` reads compiled-in version (set via `ldflags` at build)
2. Fetches latest release from `github.com/meop/ghpm`
3. Downloads correct asset for current platform
4. Replaces running binary in-place (write to temp, rename over current executable via `os.Executable()`)
5. Prints new version on success

Build pipeline: `go build -ldflags "-X main.version=<tag>"`

---

## Phase 10: Install Scripts

### 10.1 `install.sh`

```sh
# 1. Detect platform (OS + ARCH)
# 2. Fetch latest ghpm release from github.com/meop/ghpm via curl (GitHub API)
# 3. Download and extract binary to ~/.local/bin
# 4. Check if `gh` is installed
#    - If not found, prompt user: "gh not found. Install gh CLI? [y/N]"
#    - If yes, install gh via its official method, then bootstrap via: ghpm install gh
#    - gh becomes the first managed package in the manifest
# 5. Print PATH notes for ~/.local/bin and ~/.ghpm/bin
```

### 10.2 `install.ps1`

Same logic, PowerShell equivalents. Installs to `%USERPROFILE%\.local\bin`.

---

## Phase 11: Build & Release

### 11.1 Makefile

Targets:
- `build` — build for current platform
- `build-all` — cross-compile for `{linux,darwin,windows}/{amd64,arm64}`
- `test` — run tests
- `lint` — run `golangci-lint`
- `install` — build and install to `$GOPATH/bin`

### 11.2 GitHub Actions

- On tag push (`v*`): build all platforms, create GitHub Release, upload assets
- On PR: run `lint` + `test`

### 11.3 GoReleaser

Use [goreleaser/goreleaser](https://github.com/goreleaser/goreleaser) to automate cross-compilation and release publishing. ghpm itself becomes a good test case for its own release strategy.

---

## Phase 12: Testing

### 12.1 Unit tests

| Package | What to test |
|---|---|
| `internal/asset` | Asset matching: given asset names + platform + priorities, verify correct selection |
| `internal/config` | Manifest load/save, `@version` parsing, settings defaults |
| `internal/gh` | Mock `exec.Command` to test parsing of `gh` JSON output |
| `internal/parallel` | Worker pool: verify all tasks complete, results match, cancel works |

### 12.2 Integration tests

- Use a real public repo (e.g., `junegunn/fzf`) with `gh` in CI
- Test full `install` → `list` → `info` → `uninstall` cycle
- Test `outdated` against a known pinned version
- Test parallel `install` of multiple packages

### 12.3 Dry-run tests

Every command should be testable in `--dry-run` mode without side effects.

---

## Implementation Order

Ordered to minimize blocking dependencies and deliver a usable tool incrementally:

| Step | Deliverable | Depends on |
|---|---|---|
| 1 | Go module init, cobra scaffold, `--version` flag | — |
| 2 | `internal/config` — manifest load/save, dir creation, settings | Step 1 |
| 3 | `internal/gh` — gh wrapper (list releases, download asset) | Step 1 |
| 4 | `internal/asset` — platform matching, extraction, SHA verify | Step 1 |
| 5 | `internal/parallel` — worker pool with orchestrator | Step 1 |
| 6 | Package name resolution — aliases fetch, `@version` parsing | Steps 2, 3 |
| 7 | `ghpm install <names>` — end-to-end first install (parallel) | Steps 2–6 |
| 8 | `ghpm list` | Step 2 |
| 9 | `ghpm info <names>` | Steps 3, 6 |
| 10 | `ghpm download <names>` | Steps 3, 4 |
| 11 | `ghpm uninstall <names>` | Steps 2, 7 |
| 12 | `ghpm outdated` | Steps 2, 3 |
| 13 | `ghpm update [names]` (parallel, uses `asset_pattern`) | Steps 7, 12 |
| 14 | `ghpm clean [--all]` | Step 2 |
| 15 | `ghpm upgrade` (self-update) | Steps 3, 4 |
| 16 | `ghpm doctor` | Steps 2, 3 |
| 17 | `--dry-run` + confirmation prompts | All commands |
| 18 | `install.sh` / `install.ps1` | Step 15 |
| 19 | CI (GitHub Actions), GoReleaser, cross-compilation | Step 15 |
| 20 | Tests (unit + integration) | All steps |

---

## Resolved Decisions

| Topic | Decision |
|---|---|
| Version syntax | Homebrew-style `name@version` instead of `--version` flag |
| Versioned binary naming | `@` separator: `fzf@0.70.0` (not `-`). Consistent with manifest key syntax and avoids ambiguity with hyphenated package names like `lazy-git` |
| Manifest format | JSON — stdlib only, no comments needed (machine-managed) |
| Package aliases | Remote `aliases.yaml` from `github.com/meop/ghpm-config`, cached per session |
| Asset selection | Store chosen filename in manifest (`asset_pattern`) for repeatable updates |
| Platform priorities | Windows: MSVC > GNU; Linux: GNU > Musl. Configurable in `settings.json` |
| Parallelism | 5 workers default, configurable in `settings.json` |
| Manifest concurrency | Orchestrator goroutine owns writes, `sync.Mutex` on `SaveManifest()` |
| SHA verification | On by default, skip with `--no-verify` or `settings.no_verify` |
| Multiple binaries in archive | Pick one (prompt if ambiguous), install one binary per package |
| `gh` auth | Public repos need no auth. ghpm warns if `gh` not authenticated for private repos |
| PATH management | Install scripts print a note about adding `~/.ghpm/bin` to PATH; they do not modify shell rc or registry automatically |

---

## Addendum: Package Name Constraints and Deduplication

### A.1 Package names must be simple, valid filenames

The `<name>` in every `ghpm` command must be a simple, filesystem-safe string with no slashes or path separators. This name becomes the binary filename in `~/.ghpm/bin/`. Names like `cli/cli`, `github/cli/cli`, or `github.com/cli/cli` are **not accepted** — only simple names like `gh`, `fzf`, `ripgrep`.

Input forms and how they are handled:

| Input | Behavior |
|---|---|
| `ghpm install gh` | Look up `gh` in aliases → resolve to `github.com/cli/cli` |
| `ghpm install fzf@0.70` | Look up `fzf` in aliases → resolve to `github.com/junegunn/fzf`, pin version `0.70` |
| `ghpm install mytool` | No alias match → `gh search repos mysearch` → prompt user to pick a repo |
| `ghpm install cli/cli` | **Rejected** — not a valid filename. Error: "name must be a simple filename (no slashes)" |

### A.2 Resolution flow (updated)

1. **Validate name**: must be a simple filename (no `/`, no spaces, must be usable as a binary name). Reject immediately if invalid.
2. **Manifest lookup**: already installed → use stored `source` URI.
3. **Remote aliases**: look up simple name in `aliases.yaml`.
4. **GitHub search fallback**: if no alias, run `gh search repos <name>`, present results, prompt user to pick one.
5. **Deduplication check**: before proceeding with install, scan the manifest for any existing entry whose `source` matches the resolved URI. If found, inform the user that this package is already installed under a different name (e.g., `"github.com/cli/cli is already installed as 'gh'"`) and abort or offer to update instead.

There is **no** `owner/repo` shorthand or `github.com/` full URI input path. The only way to resolve a name is aliases or search. This keeps the package namespace flat and predictable.

### A.3 Updated manifest schema

```json
{
  "packages": {
    "gh": {
      "source": "github.com/cli/cli",
      "version": "2.67.0",
      "versioned": false,
      "asset_pattern": "gh_2.67.0_linux_amd64.tar.gz",
      "binary_name": "gh",
      "installed_at": "2026-04-23T10:00:00Z"
    }
  }
}
```

New field:

- **`binary_name`**: the actual executable filename found inside the release archive. This is stored because the extracted binary may not always match the package name exactly (e.g., package `ripgrep` but binary is `rg`). On `update`, this is used as a hint to find the correct binary in the new release's archive.

The manifest is keyed by the simple package name (what the user typed). The `source` field stores the full URI for deduplication checks.

### A.4 Binary discovery during extraction

After downloading and extracting a release archive:

1. **Try exact match**: look for a file matching the package name (e.g., `fzf` for `ghpm install fzf`).
2. **Try alias-aware match**: if the alias maps to a known binary name, prefer that.
3. **If multiple candidates**: prompt the user to select from numbered list of executables found in the archive.
4. **If no candidates**: error out — no usable binary found.
5. **Store `binary_name`**: whatever was chosen is saved in the manifest for future update operations.

### A.5 Update operation uses stored context

When `ghpm update` runs for a package:

1. Use stored `source` to fetch latest release.
2. Use stored `asset_pattern` to prefer the same asset naming convention in the new release.
3. Use stored `binary_name` to find the correct executable inside the extracted archive.
4. If any of these fail to produce a 1:1 match (renamed binary, changed archive structure), prompt the user to clarify — just like a fresh install, but with the stored values as defaults.
5. Update `asset_pattern` and `binary_name` in the manifest after successful update.

### A.6 Deduplication rules

- **Install**: if the resolved `source` URI already exists in the manifest under any key, warn and skip (or offer update).
- **Manifest key is always the simple name**: `gh`, not `cli/cli` or `github.com/cli/cli`.
- **Same repo, different alias**: if `aliases.yaml` has both `rg` and `ripgrep` mapping to `github.com/BurntSushi/ripgrep`, whichever is installed first wins. The second attempt warns that it's already installed and shows the existing name.
