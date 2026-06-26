#!/usr/bin/env bash
# Zebracat CLI installer.
#   curl -fsSL https://raw.githubusercontent.com/zebracatai/zebracat-cli/main/install.sh | bash
#
# Installs the `zebracat` binary into ~/.local/bin. Binaries are pulled from the
# project's GitHub Releases, so this script works unchanged from any host you put
# it on (e.g. a branded https://get.zebracat.ai/install.sh on your own CDN).
#
# Env overrides:
#   ZEBRACAT_BIN_DIR          install dir (default: ~/.local/bin)
#   ZEBRACAT_INSTALL_VERSION  pin a release tag (e.g. v0.1.0; default: latest)
set -euo pipefail

REPO="zebracatai/zebracat-cli"
BIN="zebracat"
BIN_DIR="${ZEBRACAT_BIN_DIR:-$HOME/.local/bin}"

purple() { printf '\033[38;5;141m%s\033[0m\n' "$1"; }
err() { printf '\033[31merror:\033[0m %s\n' "$1" >&2; exit 1; }

purple "🦓 Installing the Zebracat CLI…"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) err "unsupported architecture: $arch" ;;
esac
case "$os" in
  linux|darwin) ;;
  *) err "unsupported OS: $os (Windows: use the .exe from the Releases page)" ;;
esac

command -v curl >/dev/null 2>&1 || err "curl is required"

tag="${ZEBRACAT_INSTALL_VERSION:-}"
if [ -z "$tag" ]; then
  tag="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep -m1 '"tag_name"' | cut -d'"' -f4)"
fi
[ -n "$tag" ] || err "could not determine the latest release (none published yet?)"

asset="${BIN}_${os}_${arch}.tar.gz"
url="https://github.com/${REPO}/releases/download/${tag}/${asset}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

purple "Downloading ${tag} (${os}/${arch})…"
curl -fsSL "$url" -o "$tmp/$asset" || err "download failed: $url"
tar -xzf "$tmp/$asset" -C "$tmp"

mkdir -p "$BIN_DIR"
install -m 0755 "$tmp/$BIN" "$BIN_DIR/$BIN" 2>/dev/null || { mv "$tmp/$BIN" "$BIN_DIR/$BIN"; chmod 0755 "$BIN_DIR/$BIN"; }

purple "✓ Installed to $BIN_DIR/$BIN"
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) printf '\033[33m!\033[0m Add %s to your PATH:\n    export PATH="%s:$PATH"\n' "$BIN_DIR" "$BIN_DIR" ;;
esac
"$BIN_DIR/$BIN" version || true
purple "Run 'zebracat auth login' to get started."
