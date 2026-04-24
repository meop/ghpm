#!/usr/bin/env sh
set -e

GHPM_REPO='meop/ghpm'
GH_REPO='cli/cli'
INSTALL_DIR="${GHPM_INSTALL_DIR:-$HOME/.local/bin}"
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
  RELEASE_JSON=$(curl -fsSL "https://api.github.com/repos/$1/releases/latest")
}

release_tag() {
  printf '%s' "$RELEASE_JSON" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/'
}

release_asset_url() {
  local url
  url=$(printf '%s' "$RELEASE_JSON" | grep '"browser_download_url"' | grep "\"$1\"" | head -1 | sed 's/.*"browser_download_url": *"\([^"]*\)".*/\1/')
  if [ -z "$url" ]; then
    echo "Could not find asset matching '$1'"
    exit 1
  fi
  printf '%s' "$url"
}

install_from_release() {
  local pattern="$1" binary="$2" dest="$3"
  local tmp url
  tmp=$(mktemp -d)
  url=$(release_asset_url "$pattern")
  curl -fsSL "$url" -o "$tmp/pkg"
  case "$url" in
    *.tar.gz|*.tgz) tar xzf "$tmp/pkg" -C "$tmp" ;;
    *.zip)          unzip -q "$tmp/pkg" -d "$tmp" ;;
  esac
  local found
  found=$(find "$tmp" -name "$binary" -type f | head -1)
  mkdir -p "$dest"
  mv "$found" "$dest/$binary"
  chmod +x "$dest/$binary"
  rm -rf "$tmp"
}

# Install ghpm
echo "Fetching latest ghpm release: github.com/$GHPM_REPO"
fetch_release "$GHPM_REPO"
GHPM_TAG=$(release_tag)
install_from_release "ghpm-.*-${OS_TAG}-${ARCH_TAG}.tar.gz" 'ghpm' "$INSTALL_DIR"
echo "Installed ghpm $GHPM_TAG"

# Install gh (bootstrap — ghpm needs it to operate)
echo "Fetching latest gh release: github.com/$GH_REPO"
fetch_release "$GH_REPO"
GH_TAG=$(release_tag)
case "$OS_TAG" in
  linux)  install_from_release "gh_.*_linux_${ARCH_TAG}.tar.gz" 'gh' "$GHPM_BIN" ;;
  darwin) install_from_release "gh_.*_macOS_${ARCH_TAG}.zip"    'gh' "$GHPM_BIN" ;;
esac
echo "Installed gh $GH_TAG"

# Authenticate gh and register it in ghpm manifest
export PATH="$INSTALL_DIR:$GHPM_BIN:$PATH"
echo 'Authenticating gh...'
gh auth login </dev/tty
echo 'Registering gh in ghpm manifest...'
ghpm install gh </dev/tty

check_path() {
  case ":$PATH:" in
    *":$1:"*) ;;
    *)
      echo "NOTE: $1 is not in your PATH."
      echo "  Add it with: export PATH=\"\$PATH:$1\""
      ;;
  esac
}

echo ''
check_path "$INSTALL_DIR"
check_path "$GHPM_BIN"
