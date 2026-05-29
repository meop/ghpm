# CLAUDE.md

## After every code change

Run all four — do not skip any:

```sh
go build -o ghpm ./cmd/ghpm
go test ./...
gofmt -w .
golangci-lint run ./...
```

## Keeping this file current

Update this file whenever you make changes in these categories — do not wait to be asked:

- **Package added, removed, or renamed** → Project structure section
- **Manifest schema changes** → Manifest and disk layout section
- **Install/sync/tidy flow changes** → Key flows section
- **New or changed conventions** → Conventions or Print formatting section

Keep descriptions conceptual — architecture and intent, not function signatures. If you notice a discrepancy between this file and the code, fix the file immediately.

Update `README.md` when user-visible behavior changes: new flags or subcommands, changed command syntax, new install steps, or changed output format. README is user-facing; do not document internals there.

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

- `cmd/ghpm/` — entry point
- `internal/cli/` — subcommands; `helpers.go` has shared shim-naming and confirm logic
- `internal/config/` — manifest (JSON), settings, name resolution, semver, file locking
- `internal/gh/` — all GitHub interaction via `gh` CLI (`os/exec`), never a Go SDK
- `internal/asset/` — asset scoring/selection, extraction, binary/font discovery, name conflict resolution; `prompt.go` has shared stdin prompt helpers
- `internal/ioutils/` — low-level stdin helpers (`ReadLine`, `ReadSingle`) and `ErrSkip` sentinel
- `internal/shim/` — shim creation/removal (symlink on Unix, stamped .exe on Windows)
- `internal/store/` — `~/.ghpm/` path helpers
- `internal/parallel/` — bounded worker pool used for parallel download+extract

## Manifest and disk layout

Manifest lives at `~/.ghpm/manifest.json`. Each installed package entry records its pin, resolved version, which release asset(s) were used, and the bin shim names and/or user-given font names extracted from them. A package may contain bins, fonts, or both — and a single asset entry can carry both a `bin` and a `font` map (e.g. an archive shipping a CLI plus bundled fonts). Versioned installs use an `@` suffix as the key (`fzf@14`).

Extracted content lives under `~/.ghpm/extract/<key>/<version>/`. Downloaded assets are cached under `~/.ghpm/download/`. Shims land in `~/.ghpm/bin/` (which should be on `PATH`).

## Key flows

**Asset selection** (`internal/asset/`) scores release assets by platform/arch/extension and auto-picks when unambiguous; otherwise prompts. After extraction, bins are discovered by ELF/Mach-O/PE magic and presented for selection and optional rename. Proposed shim names and font user-given names are checked against all other installed packages; conflicts are mandatory renames, non-conflicts are optional.

**Add** — per arg: resolve source, fetch release, select asset(s), confirm → parallel download+extract → per result: select and name bins and/or fonts per asset (with conflict detection) → show summary table, confirm → create shims + install fonts + write manifest. Bins and fonts are tracked per asset, so one package can carry both.

**Sync** — batch version check, then for outdated packages: fetch release, re-select asset by hint, parallel download+extract → rebuild shims (conflict check) and reinstall fonts (conflict check) → write manifest.

## Print formatting

- No blank line at the start or end of any command's output
- Blank lines only between logical blocks; never two in a row — this applies inside loops too
- `sep()` guards the leading blank via `hasOutput`; safe to call before any block
- After a confirm prompt returns true and the next output is a `printTitle` loop, reset `hasOutput = false` — the user's Enter already provides visual separation
- `printPass`/`printFail`/`printInfo` do not call `sep()`; safe to use directly after a prompt
- Never call `sep()` as the last statement before returning

## Conventions

- All GitHub interaction goes through `gh` CLI, never a Go SDK
- Manifest is read/written by the orchestrator goroutine only (not by parallel workers)
- Mutating commands acquire a file lock to prevent concurrent runs
- Package names must be simple identifiers (no slashes)
- Bin filenames stored in manifest include the extension (`bun.exe` on Windows, `bun` on Unix)
- Font installs on Windows require both a file copy and a registry entry; `font_windows.go` / `font_other.go` split handles this
