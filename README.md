# ghpm

A package manager that installs binaries from GitHub Releases, using `gh` as its primary interface to GitHub.

## Requirements

- [Go 1.25+](https://go.dev/dl/)
- [gh CLI](https://cli.github.com/) (authenticated for private repos)

## Install

**Linux / macOS:**

```sh
curl -fsSL https://raw.githubusercontent.com/meop/ghpm/main/install.sh | sh
```

**Windows (PowerShell):**

```powershell
Invoke-WebRequest -UseBasicParsing https://raw.githubusercontent.com/meop/ghpm/main/install.ps1 | Invoke-Expression
```

**From source:**

```sh
go install github.com/meop/ghpm/cmd/ghpm@latest
```

After installing, add `~/.ghpm/bin` to your `PATH` so installed tools are available:

```sh
export PATH="$PATH:$HOME/.ghpm/bin"
```

## Usage

```sh
ghpm install fzf              # install latest fzf
ghpm install fzf@14           # install latest fzf 14.x (tracks within major)
ghpm install fzf@14.1         # install latest fzf 14.1.x (tracks within minor)
ghpm install fzf@14.1.0       # install exact fzf 14.1.0 (static, never updates)
ghpm install fzf ripgrep bat  # install multiple in parallel

ghpm list                     # show installed packages
ghpm info fzf                 # show available releases and assets
ghpm outdated                 # check for updates

ghpm update                   # update all floating and major/minor-pinned packages
ghpm update fzf               # update specific package

ghpm uninstall fzf            # remove package
ghpm clean                    # remove unused cached assets
ghpm clean --all              # remove all cached assets

ghpm upgrade                  # upgrade ghpm itself
ghpm doctor                   # check system health
```

### Global flags

| Flag | Description |
|---|---|
| `--dry-run` | Print what would be done without executing |
| `--no-verify` | Skip SHA256 verification |

### Version pinning

| Syntax | Meaning | Updates |
|---|---|---|
| `fzf` | Latest version | Yes — `ghpm update` fetches newest release |
| `fzf@14` | Latest 14.x | Yes — within major only |
| `fzf@14.1` | Latest 14.1.x | Yes — within major.minor only |
| `fzf@14.1.0` | Exact version | Never — static pin |

Manifest key and binary name both use the constraint as written (e.g., `fzf@14`, not `fzf@14.2.1`). The actual installed version is recorded in the manifest.

### Configuration

`~/.ghpm/settings.json` is **not created by default** — ghpm runs fine without it. Create it to override any of these defaults:

```json
{
  "parallelism": 5,
  "platform_priority": {
    "linux": ["gnu", "musl"],
    "windows": ["msvc", "gnu"]
  },
  "no_verify": false,
  "alias_repos": ["github.com/meop/ghpm-config"]
}
```

| Field | Default | Description |
|---|---|---|
| `parallelism` | `5` | Max concurrent downloads |
| `platform_priority` | see above | Preferred toolchain order when multiple assets match |
| `no_verify` | `false` | Skip SHA256 verification globally |
| `alias_repos` | `["github.com/meop/ghpm-config"]` | Alias repos to fetch from; all their `aliases.yaml` files are merged |

Package aliases resolve simple names like `fzf` to GitHub repos. `ghpm update` refreshes all configured alias repos. If a name isn't found, `ghpm` searches GitHub and prompts you to pick a repo.

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
- Alias files are cached in `~/.ghpm/aliases/github.com/<owner>/<repo>/aliases.yaml`
- Binaries are installed to `~/.ghpm/bin/`
- State is tracked in `~/.ghpm/manifest.json`
- SHA256 verification runs by default when `.sha256` sidecar files are available in the release

## License

[MIT](LICENSE)
