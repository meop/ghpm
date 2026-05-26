# CLAUDE.md

Build/test/lint commands for this project.

## Build

```sh
go build -o ghpm ./cmd/ghpm
```

## Test

```sh
go test ./...
```

## Format

```sh
gofmt -w .
```

## Lint

```sh
golangci-lint run ./...
```

## Cross-compile

```sh
GOOS=darwin  GOARCH=arm64 go build -o ghpm-darwin-arm64      ./cmd/ghpm
GOOS=darwin  GOARCH=amd64 go build -o ghpm-darwin-amd64      ./cmd/ghpm
GOOS=linux   GOARCH=arm64 go build -o ghpm-linux-arm64       ./cmd/ghpm
GOOS=linux   GOARCH=amd64 go build -o ghpm-linux-amd64       ./cmd/ghpm
GOOS=windows GOARCH=arm64 go build -o ghpm-windows-arm64.exe ./cmd/ghpm
GOOS=windows GOARCH=amd64 go build -o ghpm-windows-amd64.exe ./cmd/ghpm
```

## Project structure

- `cmd/ghpm/` — entry point (main.go)
- `internal/cli/` — cobra command definitions (all subcommands)
- `internal/config/` — manifest, settings, name resolution, semver, locking
- `internal/gh/` — gh CLI wrapper (all GitHub interaction via `os/exec`)
- `internal/asset/` — asset matching, extraction, SHA verification
- `internal/shim/` — shim creation/removal (symlink on Unix, .exe on Windows via sheesh/kebab)
- `internal/store/` — path helpers for ~/.ghpm/ directories
- `internal/parallel/` — bounded worker pool

## Conventions

- All GitHub interaction goes through `gh` CLI, never a Go SDK
- Manifest is read/written by the orchestrator goroutine only (not by parallel workers)
- Mutating commands (add, sync, tidy, upgrade) acquire a file lock via `config.AcquireLock()` to prevent concurrent runs
- Package names must be simple filenames (no slashes) — enforced by `config.ValidateName`
- Versioned packages use `@` separator in manifest keys: `fzf@14`, `fzf@14.1`, `fzf@14.1.0`
- Bin names stored in manifest include the full filename (`bun.exe` on Windows, `bun` on Unix)
- Repos cached under `~/.ghpm/repo/github.com/<owner>/<repo>/repos.yaml`, refreshed only during `ghpm refresh`
