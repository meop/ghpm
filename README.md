# ghpm

A package manager that installs binaries from GitHub Releases, using `gh` as its primary interface to GitHub.

## Requirements

- [Go 1.22+](https://go.dev/dl/)
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
ghpm install fzf@0.70         # install fzf 0.70.x (tracks latest patch)
ghpm install fzf ripgrep bat  # install multiple in parallel

ghpm list                     # show installed packages
ghpm info fzf                 # show available releases and assets
ghpm outdated                 # check for updates

ghpm update                   # update all unversioned packages
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

`ghpm` supports Homebrew-style version constraints:

| Syntax | Meaning |
|---|---|
| `fzf` | Latest version, updates on `ghpm update` |
| `fzf@0.70` | Latest `0.70.x`, updates within minor |
| `fzf@0.70.0` | Exact version, never updates |

### Configuration

Optional `~/.ghpm/settings.json`:

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

Package aliases are fetched from [ghpm-config](https://github.com/meop/ghpm-config). If a name isn't found in the alias list, `ghpm` searches GitHub and prompts you to pick a repo.

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
- Releases are cached in `~/.ghpm/release/github.com/<owner>/<repo>/<version>/`
- Binaries are installed to `~/.ghpm/bin/`
- State is tracked in `~/.ghpm/manifest.json`
- SHA256 verification runs by default when `.sha256` sidecar files are available in the release

## License

[MIT](LICENSE)
