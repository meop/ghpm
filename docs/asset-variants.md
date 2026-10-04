# Release asset variants

Many releases ship more than one asset for the same operating system and
architecture, differing only in toolchain or C library. This explains how ghpm
orders those variants and why.

## How the preference is applied

Asset selection (`internal/asset/match.go`) first decides which assets match the
host's operating system, architecture, package name, and archive type. The
toolchain and archive preferences (`toolPrefs`, `extPrefs`) only order assets
that tie on those checks. When more than one variant ties, ghpm lists them in
preference order rather than choosing silently; it picks automatically only when
a single candidate remains.

| Host | Preference | Effect |
| --- | --- | --- |
| Windows | `msvc` before `gnu` | An MSVC build is listed ahead of a MinGW (GNU) build |
| Linux | `gnu` before `musl` | A glibc build is listed ahead of a musl build |
| macOS | none | macOS releases have no toolchain variants |

## Windows: MSVC before GNU

Both run on any Windows machine. The MSVC build is the platform's native
toolchain and what most projects test first. Many projects that ship both link
the MSVC runtime statically, so it needs no Visual C++ redistributable.

The GNU build is mostly historical. For years Rust's MSVC target needed
Visual Studio's build tools and, unless statically linked, the Visual C++
runtime at install time; the GNU target avoided both and could be
cross-compiled from Linux. Shared release templates copied the pair along.
Today it mainly serves people who need the MinGW ABI, such as MSYS2 users.

On Windows ARM64 the GNU-family build is `aarch64-pc-windows-gnullvm`. Rust has
no `aarch64-pc-windows-gnu`, because the GCC-based MinGW toolchain has no
complete Windows on Arm support; the LLVM-based llvm-mingw does. `gnullvm`
does not match the `gnu` preference token, so it ranks neutral, behind MSVC.

## Linux: GNU before musl

A musl asset may or may not be statically linked, and the name does not say
which:

- Some projects ship dynamically linked musl builds meant for Alpine, which
  fail on a glibc system without a musl loader. Bun, Claude Code, Fastfetch,
  OpenCode, pnpm, and PowerShell do this today.
- Some GNU builds are themselves static, and a GNU build is what most
  projects test first.

So when a release offers both, the GNU build is the safer bet to run. When a
release offers only musl, it is usually a static build meant to run anywhere;
when it offers only GNU, that is the only option. In both cases there is a
single candidate and no preference is needed.

The trade-off: a GNU build needs a glibc at least as new as the one it was built
against, so it can fail on an older distribution where a static musl build
would have run. Some projects address this by building against an old glibc
(for example bottom's `x86_64-unknown-linux-gnu-2-17` asset).

## Related

[ghpm-config](https://github.com/meop/ghpm-config) records each registry
project's release variants (`release-matrix.md`) and the rules for which
projects it lists (`release-policy.md`).
