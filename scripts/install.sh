#!/bin/sh
# Install the latest repomap release binary.
#   curl -fsSL https://raw.githubusercontent.com/branow/repomap/main/scripts/install.sh | sh
# Environment:
#   REPOMAP_INSTALL_DIR  target directory (default: /usr/local/bin, falls back
#                      to ~/.local/bin when /usr/local/bin is not writable)
#   REPOMAP_VERSION      version to install, e.g. v0.1.0 (default: latest)
set -eu

REPO="branow/repomap"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux | darwin) ;;
  *) echo "error: unsupported OS: $os (use the Windows zip from GitHub releases)" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "error: unsupported architecture: $arch" >&2; exit 1 ;;
esac

# macOS ships a single universal binary.
[ "$os" = darwin ] && arch=all

version="${REPOMAP_VERSION:-}"
if [ -z "$version" ]; then
  version=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
    grep -m1 '"tag_name"' | cut -d '"' -f 4)
fi
[ -n "$version" ] || { echo "error: could not resolve the latest version" >&2; exit 1; }

dir="${REPOMAP_INSTALL_DIR:-}"
if [ -z "$dir" ]; then
  if [ -w /usr/local/bin ] 2>/dev/null; then
    dir=/usr/local/bin
  else
    dir="$HOME/.local/bin"
  fi
fi
mkdir -p "$dir"

archive="repomap_${version#v}_${os}_${arch}.tar.gz"
url="https://github.com/$REPO/releases/download/$version/$archive"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "downloading $archive"
curl -fsSL "$url" -o "$tmp/$archive" ||
  { echo "error: no release asset at $url" >&2; exit 1; }
tar xzf "$tmp/$archive" -C "$tmp"
install -m 0755 "$tmp/repomap" "$dir/repomap"

echo "installed $dir/repomap ($version)"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "note: $dir is not on your PATH" >&2 ;;
esac
