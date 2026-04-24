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
  url=$(printf '%s' "$RELEASE_JSON" | grep '"browser_download_url"' | grep "$1" | head -1 | sed 's/.*"browser_download_url": *"\([^"]*\)".*/\1/')
  if [ -z "$url" ]; then
    echo "Could not find asset matching '$1'"
    exit 1
  fi
  printf '%s' "$url"
}

# Install ghpm
echo "Fetching latest ghpm release: github.com/$GHPM_REPO"
fetch_release "$GHPM_REPO"
GHPM_TAG=$(release_tag)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
curl -fsSL "$(release_asset_url "ghpm-.*-${OS_TAG}-${ARCH_TAG}.tar.gz")" -o "$TMP/ghpm.tar.gz"
tar xzf "$TMP/ghpm.tar.gz" -C "$TMP"
mkdir -p "$INSTALL_DIR"
cp "$TMP/ghpm" "$INSTALL_DIR/ghpm"
chmod +x "$INSTALL_DIR/ghpm"
echo "Installed ghpm $GHPM_TAG"

# Install gh (bootstrap — ghpm needs it to operate)
echo "Fetching latest gh release: github.com/$GH_REPO"
fetch_release "$GH_REPO"
GH_TAG=$(release_tag)
GH_TMP=$(mktemp -d)
case "$OS_TAG" in
  linux)
    curl -fsSL "$(release_asset_url "gh_.*_linux_${ARCH_TAG}.tar.gz")" -o "$GH_TMP/gh.tar.gz"
    tar xzf "$GH_TMP/gh.tar.gz" -C "$GH_TMP" --strip-components=2 --wildcards '*/bin/gh'
    ;;
  darwin)
    curl -fsSL "$(release_asset_url "gh_.*_macOS_${ARCH_TAG}.zip")" -o "$GH_TMP/gh.zip"
    unzip -q "$GH_TMP/gh.zip" -d "$GH_TMP"
    find "$GH_TMP" -name 'gh' -type f | head -1 | xargs -I{} mv {} "$GH_TMP/gh"
    ;;
esac
mkdir -p "$GHPM_BIN"
mv "$GH_TMP/gh" "$GHPM_BIN/gh"
chmod +x "$GHPM_BIN/gh"
rm -rf "$GH_TMP"
echo "Installed gh $GH_TAG"

# Register gh in ghpm manifest
export PATH="$INSTALL_DIR:$GHPM_BIN:$PATH"
echo 'Registering gh in ghpm manifest...'
ghpm install gh </dev/tty

echo ''
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    echo "NOTE: $INSTALL_DIR is not in your PATH."
    echo "  Add it with: export PATH=\"\$PATH:$INSTALL_DIR\""
    ;;
esac
case ":$PATH:" in
  *":$GHPM_BIN:"*) ;;
  *)
    echo "NOTE: $GHPM_BIN is not in your PATH."
    echo "  Add it with: export PATH=\"\$PATH:$GHPM_BIN\""
    ;;
esac
