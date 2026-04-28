#!/usr/bin/env sh
set -e

GHPM_REPO='meop/ghpm'
GH_REPO='cli/cli'
GHPM_BIN="$HOME/.ghpm/bin"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64)  ARCH_TAG='amd64' ;;
  aarch64|arm64) ARCH_TAG='arm64' ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
  linux)  OS_TAG='linux' ;;
  darwin) OS_TAG='darwin' ;;
  *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

RELEASE_JSON=''

fetch_release() {
  local url="https://api.github.com/repos/$1/releases/latest"
  echo "  GET $url"
  RELEASE_JSON=$(curl -fsSL "$url") || {
    echo "  failed to fetch release from $1" >&2
    exit 1
  }
}

release_tag() {
  local tag
  tag=$(printf '%s' "$RELEASE_JSON" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
  if [ -z "$tag" ]; then
    echo "  could not parse tag_name from response:" >&2
    printf '%.500s\n' "$RELEASE_JSON" >&2
    exit 1
  fi
  printf '%s' "$tag"
}

release_asset_url() {
  local url
  url=$(printf '%s' "$RELEASE_JSON" | grep '"browser_download_url"' | grep "/$1\"" | head -1 | sed 's/.*"browser_download_url": *"\([^"]*\)".*/\1/')
  if [ -z "$url" ]; then
    echo "  could not find asset matching '$1'" >&2
    echo "  available assets:" >&2
    printf '%s' "$RELEASE_JSON" | grep '"browser_download_url"' | sed 's/.*"browser_download_url": *"\([^"]*\)".*/    \1/' >&2
    exit 1
  fi
  printf '%s' "$url"
}

install_from_release() {
  local pattern="$1" binary="$2" dest="$3"
  local tmp url
  tmp=$(mktemp -d)
  url=$(release_asset_url "$pattern")
  local pkg="$tmp/pkg"
  echo "  downloading $url"
  echo "  temp dir: $tmp"
  curl -fsSL "$url" -o "$pkg" || {
    echo "  download failed: $url" >&2
    echo "  partial file: $pkg ($(ls -la "$pkg" 2>/dev/null | awk '{print $5}') bytes)" >&2
    rm -rf "$tmp"
    exit 1
  }
  echo "  downloaded $(ls -la "$pkg" | awk '{print $5}') bytes to $pkg"
  case "$url" in
    *.tar.gz|*.tgz)
      if ! tar xzf "$pkg" -C "$tmp" 2>&1; then
        echo "  tar extraction failed for $pkg" >&2
        echo "  file type: $(file "$pkg")" >&2
        echo "  file size: $(ls -la "$pkg" | awk '{print $5}') bytes" >&2
        echo "  first bytes (hex): $(od -A x -t x1z -N 16 "$pkg" | head -1)" >&2
        rm -rf "$tmp"
        exit 1
      fi
      ;;
    *.zip)
      if ! unzip -q "$pkg" -d "$tmp" 2>&1; then
        echo "  unzip failed for $pkg" >&2
        echo "  file type: $(file "$pkg")" >&2
        echo "  file size: $(ls -la "$pkg" | awk '{print $5}') bytes" >&2
        rm -rf "$tmp"
        exit 1
      fi
      ;;
  esac
  local found
  found=$(find "$tmp" -name "$binary" -type f | head -1)
  if [ -z "$found" ]; then
    echo "  binary '$binary' not found in archive" >&2
    echo "  archive contents:" >&2
    find "$tmp" -type f | sed 's|^|    |' >&2
    rm -rf "$tmp"
    exit 1
  fi
  mkdir -p "$dest"
  mv "$found" "$dest/$binary"
  chmod +x "$dest/$binary"
  echo "  installed $dest/$binary"
  rm -rf "$tmp"
}

# Install ghpm
echo "Fetching latest ghpm release: github.com/$GHPM_REPO"
fetch_release "$GHPM_REPO"
GHPM_TAG=$(release_tag)
echo "  version: $GHPM_TAG"
install_from_release "ghpm-.*-${OS_TAG}-${ARCH_TAG}.tar.gz" 'ghpm' "$GHPM_BIN"

# Install gh (bootstrap — ghpm needs it to operate)
echo "Fetching latest gh release: github.com/$GH_REPO"
fetch_release "$GH_REPO"
GH_TAG=$(release_tag)
echo "  version: $GH_TAG"
case "$OS_TAG" in
  linux)  install_from_release "gh_.*_linux_${ARCH_TAG}.tar.gz" 'gh' "$GHPM_BIN" ;;
  darwin) install_from_release "gh_.*_macOS_${ARCH_TAG}.zip"    'gh' "$GHPM_BIN" ;;
esac
export PATH="$GHPM_BIN:$PATH"
if ! gh auth status >/dev/null 2>&1; then
  echo 'Authenticating gh...'
  gh auth login </dev/tty
fi

echo ''
echo 'To activate ghpm, add ~/.ghpm/bin to PATH and source the env script:'
echo '  nu:   $env.PATH = ($env.PATH | prepend ~/.ghpm/bin); source ~/.ghpm/scripts/env.nu'
echo '  pwsh: $env:PATH = "$HOME\.ghpm\bin;$env:PATH"; . ~/.ghpm/scripts/env.ps1'
echo '  sh:   export PATH="$HOME/.ghpm/bin:$PATH" && . ~/.ghpm/scripts/env.sh'
