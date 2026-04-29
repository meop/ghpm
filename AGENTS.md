# AGENTS.md

Build/test/lint commands for this project.

## Build

```sh
make build
# or: go build -o ghpm ./cmd/ghpm
```

## Test

```sh
make test
# or: go test ./...
```

## Lint

```sh
make lint
# or: golangci-lint run ./...
```

## Cross-compile

```sh
make build-all
```

## Project structure

- `cmd/ghpm/` — entry point (main.go)
- `internal/cli/` — cobra command definitions (all subcommands)
- `internal/config/` — manifest, settings, name resolution, semver, locking
- `internal/gh/` — gh CLI wrapper (all GitHub interaction via `os/exec`)
- `internal/asset/` — asset matching, extraction, SHA verification
- `internal/shim/` — shim creation/removal (symlink on Unix, .cmd on Windows)
- `internal/store/` — path helpers for ~/.ghpm/ directories
- `internal/parallel/` — bounded worker pool

## Conventions

- All GitHub interaction goes through `gh` CLI, never a Go SDK
- Manifest is read/written by the orchestrator goroutine only (not by parallel workers)
- Mutating commands (install, update, clean) acquire a file lock via `config.AcquireLock()` to prevent concurrent runs
- Package names must be simple filenames (no slashes) — enforced by `config.ValidateName`
- Versioned binaries use `@` separator: `fzf@0.70.0`
- Repos cached under `~/.ghpm/repos/github.com/<owner>/<repo>/repos.yaml`, refreshed only during `ghpm update`
