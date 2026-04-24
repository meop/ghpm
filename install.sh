#!/usr/bin/env sh
set -e

GHPM_REPO="meop/ghpm"
GH_REPO="cli/cli"
INSTALL_DIR="${GHPM_INSTALL_DIR:-$HOME/.local/bin}"
GHPM_BIN="$HOME/.ghpm/bin"

# Detect OS and ARCH
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64)  ARCH_TAG="amd64" ;;
  aarch64|arm64) ARCH_TAG="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

case "$OS" in
  linux)   OS_TAG="linux" ;;
  darwin)  OS_TAG="darwin" ;;
  *)
    echo "Unsupported OS: $OS"
    exit 1
    ;;
esac

# --- helper: download latest release asset from GitHub ---
# Usage: gh_download <owner/repo> <asset_glob> <dest_file>
gh_download() {
  local repo="$1" pattern="$2" dest="$3"
  local api_url="https://api.github.com/repos/$repo/releases/latest"
  local asset_url
  asset_url=$(curl -fsSL "$api_url" \
    | grep '"browser_download_url"' \
    | grep "$pattern" \
    | head -1 \
    | sed 's/.*"browser_download_url": "\(.*\)".*/\1/')
  if [ -z "$asset_url" ]; then
    echo "Could not find asset matching '$pattern' in $repo latest release"
    exit 1
  fi
  curl -fsSL "$asset_url" -o "$dest"
}

# --- Install ghpm ---
echo "Fetching latest ghpm release..."
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

ASSET_PATTERN="ghpm-.*-${OS_TAG}-${ARCH_TAG}.tar.gz"
gh_download "$GHPM_REPO" "$ASSET_PATTERN" "$TMP/ghpm.tar.gz"
tar xzf "$TMP/ghpm.tar.gz" -C "$TMP"

mkdir -p "$INSTALL_DIR"
cp "$TMP/ghpm" "$INSTALL_DIR/ghpm"
chmod +x "$INSTALL_DIR/ghpm"
echo "Installed ghpm to $INSTALL_DIR/ghpm"

# --- Check / install gh CLI ---
if ! command -v gh >/dev/null 2>&1; then
  printf "gh CLI not found. Install it from its GitHub release now? [y/N] "
  read -r REPLY </dev/tty
  if [ "$REPLY" = "y" ] || [ "$REPLY" = "Y" ]; then
    GH_TMP=$(mktemp -d)
    trap 'rm -rf "$GH_TMP"' EXIT

    case "$OS_TAG" in
      linux)
        GH_ASSET="gh_.*_linux_${ARCH_TAG}.tar.gz"
        gh_download "$GH_REPO" "$GH_ASSET" "$GH_TMP/gh.tar.gz"
        tar xzf "$GH_TMP/gh.tar.gz" -C "$GH_TMP" --strip-components=2 --wildcards '*/bin/gh'
        mkdir -p "$GHPM_BIN"
        mv "$GH_TMP/gh" "$GHPM_BIN/gh"
        chmod +x "$GHPM_BIN/gh"
        ;;
      darwin)
        GH_ASSET="gh_.*_macOS_${ARCH_TAG}.zip"
        gh_download "$GH_REPO" "$GH_ASSET" "$GH_TMP/gh.zip"
        unzip -q "$GH_TMP/gh.zip" -d "$GH_TMP"
        mkdir -p "$GHPM_BIN"
        find "$GH_TMP" -name 'gh' -type f -exec mv {} "$GHPM_BIN/gh" \;
        chmod +x "$GHPM_BIN/gh"
        ;;
    esac

    echo "Installed gh to $GHPM_BIN/gh"
  fi
fi

# --- Remind about PATH ---
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    echo ""
    echo "NOTE: $INSTALL_DIR is not in your PATH."
    echo "  Add it with:  export PATH=\"\$PATH:$INSTALL_DIR\""
    ;;
esac

case ":$PATH:" in
  *":$GHPM_BIN:"*) ;;
  *)
    echo "NOTE: $GHPM_BIN is not in your PATH."
    echo "  Add it with:  export PATH=\"\$PATH:$GHPM_BIN\""
    ;;
esac
