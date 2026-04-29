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

After installing, add the shell hook to your config:

**zsh / bash** — add to `~/.zshrc` or `~/.bashrc`:
```sh
eval "$(ghpm init)"
```

**Nushell** — add to `~/.config/nushell/config.nu`:
```nu
ghpm init nu | save -f ([$NU_VENDOR_DATA_DIR, 'ghpm.nu'] | path join)
```

**PowerShell / pwsh** — add to `$PROFILE`:
```powershell
Invoke-Expression (ghpm init powershell)
```

The hook sources the generated env file and defines a `ghpm reload` alias for refreshing your PATH after installs.

## Usage

```sh
ghpm install fzf              # install latest fzf
ghpm install fzf@14           # install latest fzf 14.x (tracks within major)
ghpm install fzf@14.1         # install latest fzf 14.1.x (tracks within minor)
ghpm install fzf@14.1.0       # install exact fzf 14.1.0 (static, never updates)
ghpm install fzf ripgrep bat  # install multiple in parallel
ghpm install --force fzf      # reinstall even if already installed

ghpm list                     # show installed packages
ghpm search fzf               # search cached repos by name or source
ghpm show fzf                 # show available releases and assets
ghpm outdated                 # check for updates

ghpm update                   # update all floating and major/minor-pinned packages
ghpm update fzf               # update specific package

ghpm download fzf             # download release asset to cache without installing
ghpm download --path /tmp fzf # download release asset to a specific directory

ghpm uninstall fzf            # remove package
ghpm clean                    # remove unused cached assets and orphaned package dirs
ghpm clean --all              # remove all cached assets

ghpm init                     # output posix shell hook for eval
ghpm init nu                  # output shell hook for eval (nu/nushell/pwsh/powershell, else posix)
ghpm upgrade                  # upgrade ghpm itself and managed gh
ghpm doctor                   # check system health
```

### Global flags

| Flag | Description |
|---|---|
| `--dry-run` | Print what would be done without executing |
| `--no-verify` | Skip SHA256 verification |
| `--yes`, `-y` | Skip confirmation prompts |

### Version pinning

| Syntax | Meaning | Updates |
|---|---|---|
| `fzf` | Latest version | Yes — `ghpm update` fetches newest release |
| `fzf@14` | Latest 14.x | Yes — within major only |
| `fzf@14.1` | Latest 14.1.x | Yes — within major.minor only |
| `fzf@14.1.0` | Exact version | Never — static pin |

Manifest key and directory name both use the constraint as written (e.g., `fzf@14`, not `fzf@14.2.1`). The actual installed version is recorded in the manifest.

### Portable app support

ghpm extracts archives into `~/.ghpm/extracts/<name>/` and discovers the bin directory automatically. Env scripts add each package's bin directory to `PATH`. GitHub releases are portable apps — binaries locate their own resources via paths relative to the executable, so no other env vars are needed.

### Configuration

`~/.ghpm/settings.json` is **not created by default** — ghpm runs fine without it. Create it to override any of these defaults:

```json
{
  "cache_ttl": "5m",
  "color": {
    "fail": "red",
    "info": "blue",
    "new": "green",
    "old": "red",
    "pass": "green",
    "warn": "yellow"
  },
  "no_color": false,
  "no_verify": false,
  "num_parallel": 5,
  "plat_priority": {
    "linux": ["gnu", "musl"],
    "windows": ["msvc", "gnu"]
  },
  "repo_sources": ["github.com/meop/ghpm-config"]
}
```

| Field | Default | Description |
|---|---|---|
| `cache_ttl` | `"5m"` | How long cached version data stays fresh before re-fetching |
| `color` | see above | Output colors by message type |
| `no_color` | `false` | Disable colored output |
| `no_verify` | `false` | Skip SHA256 verification globally |
| `num_parallel` | `5` | Max concurrent downloads |
| `plat_priority` | see above | Preferred toolchain order when multiple assets match |
| `repo_sources` | `["github.com/meop/ghpm-config"]` | Repo sources to fetch from; all their `repos.yaml` files are merged |

Package repos map simple names like `fzf` to GitHub repos. `ghpm update` refreshes all configured repo sources. If a name isn't found, `ghpm` searches GitHub and prompts you to pick a repo.

## Build from source

```sh
make build          # build for current platform
make build-all      # cross-compile for all platforms
make test           # run tests
make lint           # run golangci-lint
make install        # install to $GOPATH/bin
```

Releases are built with [GoReleaser](https://goreleaser.com/) via GitHub Actions on tag push.

## How it works

- All GitHub interaction goes through the `gh` CLI — no GitHub SDK
- Release assets are cached in `~/.ghpm/releases/github.com/<owner>/<repo>/<version>/`
- Packages are extracted to `~/.ghpm/extracts/<name>/` with full directory structure
- Path scripts (`path.sh`, `path.nu`, `path.ps1`) are generated in `~/.ghpm/scripts/`
- State is tracked in `~/.ghpm/manifest.json`
- SHA256 verification runs by default when `.sha256` sidecar files are available in the release
- Path scripts are regenerated after every install/update/uninstall/clean

## License

[MIT](LICENSE.txt)
