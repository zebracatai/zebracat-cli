#!/bin/sh
# Zebracat CLI installer (POSIX sh: works with bash, dash, and BusyBox ash).
#   curl -fsSL https://cli.zebracat.ai | sh
#   wget -qO- https://cli.zebracat.ai | sh
#   (same file: https://raw.githubusercontent.com/zebracatai/zebracat-cli/main/install.sh)
# Windows: install.ps1 in PowerShell, or `npm install -g @zebracat/cli`.
#
# Installs the `zebracat` binary into ~/.local/bin. Binaries are pulled from the
# project's GitHub Releases, so this script works unchanged from any host you put
# it on; https://cli.zebracat.ai serves this file live from main.
#
# Env overrides:
#   ZEBRACAT_BIN_DIR          install dir (default: ~/.local/bin)
#   ZEBRACAT_INSTALL_VERSION  pin a release tag (e.g. v0.7.0; default: latest)
#   ZEBRACAT_RELEASES_URL     releases host (default: https://github.com; for tests)
set -eu

REPO="zebracatai/zebracat-cli"
BIN="zebracat"
BIN_DIR="${ZEBRACAT_BIN_DIR:-$HOME/.local/bin}"
GH="${ZEBRACAT_RELEASES_URL:-https://github.com}"
GH="${GH%/}"
PS1_URL="https://raw.githubusercontent.com/${REPO}/main/install.ps1"

purple() { printf '\033[38;5;141m%s\033[0m\n' "$1"; }
err() { printf '\033[31merror:\033[0m %s\n' "$1" >&2; exit 1; }

purple "🦓 Installing the Zebracat CLI…"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$os" in
  linux|darwin) ;;
  mingw*|msys*|cygwin*|windows*)
    err "this script is for Linux and macOS. On Windows, run in PowerShell: irm ${PS1_URL} | iex   (or: npm install -g @zebracat/cli)" ;;
  *) err "unsupported OS: $os (Linux and macOS only; or: npm install -g @zebracat/cli)" ;;
esac
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) err "unsupported architecture: $arch" ;;
esac

# dl URL FILE downloads URL to FILE; final_url URL prints where URL redirects to.
if command -v curl >/dev/null 2>&1; then
  dl() { curl -fsSL "$1" -o "$2"; }
  # Following the redirect needs no API call (no 60/h anonymous rate limit).
  final_url() { curl -fsSLI -o /dev/null -w '%{url_effective}' "$1"; }
elif command -v wget >/dev/null 2>&1; then
  dl() { wget -q -O "$2" "$1"; }
  # wget -S prints each response's headers; the last Location is the release tag.
  final_url() { wget -S -O /dev/null "$1" 2>&1 | awk 'tolower($1) == "location:" { l = $2 } END { print l }' | tr -d '\r'; }
else
  err "needs curl or wget to download the CLI (e.g. apk add curl / apt-get install curl)"
fi
command -v tar >/dev/null 2>&1 || err "needs tar to unpack the download"

tag="${ZEBRACAT_INSTALL_VERSION:-}"
if [ -z "$tag" ]; then
  latest="$(final_url "${GH}/${REPO}/releases/latest")" \
    || err "could not reach GitHub to find the latest release"
  case "$latest" in
    */releases/tag/*) tag="${latest##*/}" ;;
  esac
fi
[ -n "$tag" ] || err "could not determine the latest release (none published yet?)"

asset="${BIN}_${os}_${arch}.tar.gz"
url="${GH}/${REPO}/releases/download/${tag}/${asset}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

purple "Downloading ${tag} (${os}/${arch})…"
dl "$url" "$tmp/$asset" || err "download failed: $url"

# Check the archive against the release's checksums.txt before installing it.
# No checksums file or no sha256 tool: refuse rather than install unchecked.
sums_url="${GH}/${REPO}/releases/download/${tag}/checksums.txt"
dl "$sums_url" "$tmp/checksums.txt" || err "could not download checksums.txt: $sums_url"
want="$(awk -v f="$asset" '$2 == f { print $1 }' "$tmp/checksums.txt")"
[ -n "$want" ] || err "checksums.txt has no line for $asset; not installing"
if command -v sha256sum >/dev/null 2>&1; then
  got="$(sha256sum "$tmp/$asset" | awk '{ print $1 }')"
elif command -v shasum >/dev/null 2>&1; then
  got="$(shasum -a 256 "$tmp/$asset" | awk '{ print $1 }')"
else
  err "need sha256sum or shasum to check the download"
fi
[ "$(printf '%s' "$want" | tr 'A-F' 'a-f')" = "$(printf '%s' "$got" | tr 'A-F' 'a-f')" ] \
  || err "checksum mismatch for $asset; the download was not installed"
purple "✓ Checksum verified"

tar -xzf "$tmp/$asset" -C "$tmp"
[ -f "$tmp/$BIN" ] || err "the archive did not contain $BIN"

mkdir -p "$BIN_DIR"
install -m 0755 "$tmp/$BIN" "$BIN_DIR/$BIN" 2>/dev/null || { mv "$tmp/$BIN" "$BIN_DIR/$BIN"; chmod 0755 "$BIN_DIR/$BIN"; }

purple "✓ Installed to $BIN_DIR/$BIN"
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) printf '\033[33m!\033[0m Add %s to your PATH:\n    export PATH="%s:$PATH"\n' "$BIN_DIR" "$BIN_DIR" ;;
esac
"$BIN_DIR/$BIN" version || true
purple "Run 'zebracat auth login' to get started."
