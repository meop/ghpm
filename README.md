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

After installing, activate ghpm by adding `~/.ghpm/bin` to PATH and sourcing the env script:

```sh
export PATH="$HOME/.ghpm/bin:$PATH"
echo 'source ~/.ghpm/scripts/env.sh' >> ~/.bashrc
echo 'source ~/.ghpm/scripts/env.sh' >> ~/.zshrc
```

Or for Nushell / PowerShell:

```sh
$env.PATH = ($env.PATH | prepend ~/.ghpm/bin)
# Add to your nu.config: source ~/.ghpm/scripts/env.nu
Add-Content $PROFILE '$env:PATH = "$env:USERPROFILE\.ghpm\bin;$env:PATH"; . ~/.ghpm/scripts/env.ps1'
```

Run `ghpm init` to generate the env script files.

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
ghpm info fzf                 # show available releases and assets
ghpm outdated                 # check for updates

ghpm update                   # update all floating and major/minor-pinned packages
ghpm update fzf               # update specific package

ghpm download fzf             # download release asset to cache without installing
ghpm download --path /tmp fzf # download release asset to a specific directory

ghpm uninstall fzf            # remove package
ghpm clean                    # remove unused cached assets and orphaned package dirs
ghpm clean --all              # remove all cached assets

ghpm init                     # generate shell env scripts
ghpm init --shell nu          # force generate for a specific shell
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

ghpm extracts full archives into `~/.ghpm/packages/<name>/`, preserving directory structure. It discovers `bin/`, `lib/`, `share/`, and other directories automatically and generates shell env scripts that set up `PATH`, `LD_LIBRARY_PATH`, `MANPATH`, and `XDG_DATA_DIRS` accordingly. Env scripts always prepend `~/.ghpm/bin/` to PATH so both `ghpm` and `gh` are available.

This means ghpm works with both single-binary tools (like `fzf`) and multi-file tools (like editors with `bin/`, `lib/`, `share/`, `runtime/` directories).

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
- Packages are extracted to `~/.ghpm/packages/<name>/` with full directory structure
- Shell env scripts (`env.sh`, `env.nu`, `env.ps1`) are generated in `~/.ghpm/scripts/`
- State is tracked in `~/.ghpm/manifest.json`
- SHA256 verification runs by default when `.sha256` sidecar files are available in the release
- Env scripts are regenerated after every install/update/uninstall/clean

## License

[MIT](LICENSE.txt)
