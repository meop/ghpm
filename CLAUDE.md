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
- `internal/asset/` — asset matching, extraction, SHA verification, binary/font discovery
- `internal/shim/` — shim creation/removal (symlink on Unix, .exe on Windows via sheesh/kebab)
- `internal/store/` — path helpers for ~/.ghpm/ directories
- `internal/parallel/` — bounded worker pool
- `internal/ghbin/` — gh CLI binary discovery (PATH then ~/.ghpm/bin/gh)
- `internal/ioutils/` — stdin readline helper
- `internal/version/` — version string normalization (strips leading non-digit prefix)

## Manifest data model

Location: `~/.ghpm/manifest.json`

```json
{
  "repo": { "fzf": "github.com/junegunn/fzf" },
  "extract": {
    "fzf": {
      "pin": "latest",
      "version": "0.56.0",
      "asset": {
        "fzf-0.56.0-linux_amd64.tar.gz": { "bin": { "fzf": "fzf" } }
      }
    },
    "nerd-fonts": {
      "pin": "latest",
      "version": "3.3.0",
      "asset": {
        "Hack.zip": {
          "font": {
            "hack":     "Hack/Hack Regular Nerd Font Complete Mono.ttf",
            "hack-bold": "Hack/Hack Bold Nerd Font Complete Mono.ttf"
          }
        },
        "FiraCode.zip": {
          "font": {
            "firacode": "FiraCode/FiraCode Regular Nerd Font Complete Mono.ttf"
          }
        }
      }
    }
  }
}
```

Key types (`internal/config/manifest.go`):
- `AssetEntry` — `Bin map[string]string` (shimName → binPath) OR `Font map[string]string` (userGivenName → fontFilePath). Never both; a package is all-bin or all-font.
- `PackageEntry` — `Pin`, `Version`, `Asset map[string]AssetEntry` (release asset filename → AssetEntry; same for both bin and font packages)
- `Manifest` — `Repos map[string]string` (name → source) + `Extracts map[string]PackageEntry`

Helper methods on `PackageEntry`:
- `BinAssetName()` — name of first asset with non-empty Bin map
- `AllBins()` — merged shimName → binPath across all assets
- `AllFonts()` — merged userGivenName → fontFilePath across all assets
- `pkgType(p)` (cli helper) — returns `"bin"` or `"font"` based on AllFonts/AllBins

## Disk layout

```
~/.ghpm/
  bin/          # shims (ghpm-managed executables on PATH)
  shim/         # sheesh runtime + kebab stamper
  extract/      # extracted package contents, permanent
    <key>/
      <version>/
        ...     # bin and font packages extracted here (same layout)
  download/     # cached release assets
    <host>/<owner>/<repo>/<version>/
      <assetName>
  repo/         # cached repo name→source YAML, refreshed by ghpm refresh
    github.com/<owner>/<repo>/repos.yaml
  manifest.json
```

Versioned package keys use `@` separator: `fzf@14`, `fzf@14.1`, `fzf@14.1.0`.

## Font support

Install: `ghpm add nerd-fonts --font hack --font fira-code`
- `--font <name>` is mutually exclusive with bin install; the arg is used to **score/select the release asset** (e.g. `"hack"` scores `"Hack.zip"` higher via asset scoring), NOT to name the font
- After asset selection, download, and extraction, font files are shown for multi-select (`SelectFonts`)
- Then `PromptFontNames` lets the user give each selected font a name (default derived from filename) — analogous to shim renaming for bins
- The user-given name becomes the key in the `font: {}` manifest map; value is the relative path to the font file in the extracted dir
- Multiple `--font` args select multiple assets; all extracted to the same `~/.ghpm/extract/<key>/<version>/` dir

Font install paths:
- Linux: `$XDG_DATA_HOME/fonts` or `~/.local/share/fonts/`
- macOS: `~/Library/Fonts/`
- Windows: `%LOCALAPPDATA%\Microsoft\Windows\Fonts\` + registry entry in `HKCU\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`

Windows registry is accessed via `golang.org/x/sys/windows/registry` (not `exec "reg"`). Platform split: `font_windows.go` (registry ops) + `font_other.go` (no-ops).

A font install is "broken" (detected by tidy) when: file missing from font dir, OR file present but registry entry missing (Windows only). Checked via `fontInstalled(fontFilePath, fontsDir)`.

## Asset selection pipeline

1. `asset.SelectAssetAuto(assets, cfg, hint, name)` — scores all release assets; returns auto-chosen if unambiguous (previous asset filename as `hint`, package name as `name` for scoring). Negative scores for wrong-platform terms disqualify regardless of positives.
2. `asset.PromptFromCandidates(ac)` — shows scored list to user if auto-selection failed or was ambiguous
3. For bins: `asset.FindBinaries(pkgDir, name)` → `asset.SelectBinaries(candidates, prevKeys)` — auto-selects if candidate set matches `prevKeys`, otherwise prompts; then optionally `PromptShimRenames`
4. For fonts: `asset.FindFonts(pkgDir)` → `asset.SelectFonts(candidates, prevFilePaths)` — auto-selects if file paths match previous; then `PromptFontNames` for user-given names

## Add flow

`ready []jobWithRelease` — bin packages (asset selected, downloaded in parallel, then FindBinaries/SelectBinaries/PromptShimRenames, shim creation, manifest write)
`fontReady []jobWithRelease` — font packages (release only, no asset pre-selected; `processFontNames` handles per-`--font`-arg asset selection, download, extract, FindFonts/SelectFonts/PromptFontNames)

Both converge into `[]shimPlan` (bins via `bins map[string]string`, fonts via `fontAssets map[string]map[string]string`) and go through one confirm prompt before the actual install phase.

## Sync flow

- Batch version check via `gh.BatchLatestVersions`
- Font packages detected by `len(pkg.AllFonts()) > 0` — each asset re-selected using stored asset filename as both hint and scoring term
- Parallel download+extract (all font assets extracted to same dir)
- Result processing: bins → FindBinaries/SelectBinaries/rebuild shims; fonts → FindFonts/SelectFonts (using prev file paths for auto-select), then preserve old user-given names where file paths match, derive names for new files

## Print formatting

- No blank line at the start or end of any command's output
- Blank lines only between logical blocks; never two in a row — this applies inside loops and nested loops too; a block that starts or ends with `sep()` inside a loop will double-blank when adjacent iterations also call `sep()`
- `sep()` in `table.go` guards the leading blank via `hasOutput`; it prints a blank only when `hasOutput` is true, then sets it true
- After `promptConfirm`/`promptInstall` returns true and the next output is a `printTitle` loop, reset `hasOutput = false` immediately — the user's Enter already provides a line break
- `printPass`/`printFail`/`printInfo` do not call `sep()`; safe to use directly after a prompt with no extra blank
- Never call `sep()` as the last statement before returning if it would be the final output

## Conventions

- All GitHub interaction goes through `gh` CLI, never a Go SDK
- Manifest is read/written by the orchestrator goroutine only (not by parallel workers)
- Mutating commands (add, sync, tidy, upgrade) acquire a file lock via `config.AcquireLock()` to prevent concurrent runs
- Package names must be simple filenames (no slashes) — enforced by `config.ValidateName`
- Versioned packages use `@` separator in manifest keys: `fzf@14`, `fzf@14.1`, `fzf@14.1.0`
- Bin names stored in manifest include the full filename (`bun.exe` on Windows, `bun` on Unix)
- Repos cached under `~/.ghpm/repo/github.com/<owner>/<repo>/repos.yaml`, refreshed only during `ghpm refresh`
