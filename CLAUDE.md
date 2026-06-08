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
- `internal/asset/` — asset scoring/selection, extraction, binary/font discovery, name conflict resolution; `prompt.go` has shared selection-read helpers
- `internal/ui/` — the single sink for all console output and prompts (deferred separators, styled lines, tables, confirm/select reads, `ErrSkip` sentinel). Imports no other internal package; color is injected via `SetColorResolver`
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

**Tidy** — removes broken installs (manifest entries whose extract or shims are missing), orphaned bin shims (files in `bin/` with no manifest entry), orphaned extracts, and orphaned cached downloads. Tidy does NOT scan the user font directory or Windows font registry for untracked fonts — fonts installed by other tools are not ghpm's responsibility.

## Print formatting

All output and prompts go through `internal/ui`, which uses **deferred separators**. This is what makes spacing robust — most of the old footguns no longer exist.

- `ui.Break()` (aliased as `sep()` in `cli`) *requests* a blank line; the blank is only emitted immediately before the next real output. It is idempotent and self-guarding: no leading blank (a Break before any output is a no-op), no double blank (consecutive Breaks collapse), no trailing blank (a Break with nothing after it never prints).
- Because of this, you can call `sep()` at the **top of any per-item loop** unconditionally — even iterations that print nothing are safe (no stray or double blank). This is the normal way to blank-separate per-package blocks; `add`/`sync`/`info`/`find` all do it.
- Blank lines appear only where code calls `Break()`. There is no "Enter provides separation" reset to manage anymore.
- **The prompt rule**: every interactive prompt is its own block — a blank line **before and after**. The only exceptions are the very start and very end of all CLI output, and they happen automatically (a leading `Break` before any output is a no-op; a trailing `Break` with nothing after it never flushes; adjacent prompts collapse to one blank). This is owned in one place: `ui.Prompt(body)` brackets `body` with a `Break` on each side. Do **not** hand-roll prompt spacing with `sep()`/`Break()` at call sites — route through `Prompt`. Non-prompt output (download/install progress) stays tight unless a block explicitly `Break`s.
- Output styling: `Out` (plain), `Info`/`Warn`/`Fail`/`Pass` (role-prefixed, colored via the injected resolver), `Table` (renders as one Break-separated block — output, not a prompt, so leading `Break` only). `cli` wraps these as `print`/`printInfo`/.../`printTable`; the `*config.Settings` arg on the `cli` wrappers is vestigial (color comes from the resolver set in `initCommand`) and ignored.
- Prompts: all of them — `ui.Confirm`, the `asset` selection/rename menus, `config.SearchGitHub` — run their interaction inside a `ui.Prompt` closure and render their header + numbered items via `ui.Menu(label, header, items)`. `Menu`'s `label` (a package name, threaded in by callers) prefixes the header (`"<pkg>: choose bin(s)"`, `"<pkg>: bin conflicts — …"`) so a prompt that interrupts the tight per-result loops names its package; pass `""` where a preceding context line already identifies it (the resolve-loop asset menu prints `pkg: repo → src`). Auto-select / auto-resolve paths return **before** the `Prompt` call, so non-prompting packages stay tight. For multi-step prompts (rename, asset show-more) the whole interaction lives in one `Prompt` body so the trailing blank lands after the last read, not between reads.
- Per-package messages inline the name (e.g., `"%s installed"`, `"%s: bin %s"`) — no per-result title line in the final install/sync/download output loops.
- `internal/ui/ui_test.go` has golden tests asserting exact blank-line placement (loops, empty iterations, post-prompt separation, tables). Add to them when changing spacing behavior.

## Conventions

- All GitHub interaction goes through `gh` CLI, never a Go SDK
- Interactive prompts must run only in the orchestrator goroutine, never inside a `parallel.Task` worker — `ui` is a shared single sink (one stdin reader, shared separator state), so concurrent prompts would interleave and race. The pattern is: parallel workers do network fetch + scoring (`SelectAssetAuto`) and return candidates; the orchestrator then prompts sequentially over the results (see `download`, `add`, `sync`)
- Manifest is read/written by the orchestrator goroutine only (not by parallel workers)
- Mutating commands acquire a file lock to prevent concurrent runs
- Package names must be simple identifiers (no slashes)
- Bin filenames stored in manifest include the extension (`bun.exe` on Windows, `bun` on Unix)
- Font installs on Windows require both a file copy and a registry entry; `font_windows.go` / `font_other.go` split handles this
