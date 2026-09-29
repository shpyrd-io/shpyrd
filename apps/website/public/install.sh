#!/bin/sh
# Installs the shpyrd CLI from the latest GitHub release (RFC-0045).
#
#   curl -fsSL https://shpyrd.io/install.sh | sh
#   curl -fsSL https://shpyrd.io/install.sh | SHPYRD_VERSION=v0.1.0 sh
#   curl -fsSL https://shpyrd.io/install.sh | SHPYRD_INSTALL_DIR=$HOME/bin sh
#
# macOS and Linux, amd64 and arm64. Verifies the SHA-256 checksum published
# with the release. On macOS you can use Homebrew instead:
#   brew install shpyrd-io/tap/shpyrd
set -eu

REPO="shpyrd-io/shpyrd"
VERSION="${SHPYRD_VERSION:-}"
INSTALL_DIR="${SHPYRD_INSTALL_DIR:-}"

say() { printf '%s\n' "$*" >&2; }
die() { say "error: $*"; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }

need curl
need tar

os="$(uname -s)"
case "$os" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) die "unsupported operating system: $os (macOS and Linux are supported; on Windows use WSL)" ;;
esac
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported architecture: $arch" ;;
esac

if [ -z "$VERSION" ]; then
  # The release redirect resolves "latest" without the API's rate limit.
  VERSION="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" | sed 's#.*/tag/##')"
  [ -n "$VERSION" ] || die "cannot determine the latest version; pass SHPYRD_VERSION=vX.Y.Z"
fi
case "$VERSION" in v*) ;; *) VERSION="v$VERSION" ;; esac

base="https://github.com/$REPO/releases/download/$VERSION"
archive="shpyrd_${VERSION}_${os}_${arch}.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

say "Downloading shpyrd $VERSION for $os/$arch..."
curl -fsSL -o "$tmp/$archive" "$base/$archive" || die "no release asset $archive at $base"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || die "checksums.txt missing from the release"

expected="$(grep " $archive\$" "$tmp/checksums.txt" | awk '{print $1}')"
[ -n "$expected" ] || die "$archive is not listed in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$archive" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')"
fi
[ "$expected" = "$actual" ] || die "checksum mismatch for $archive (expected $expected, got $actual)"

# Two binaries since v0.8.0: shpyrd (developers) and shpyrd-ctl (operators).
tar -xzf "$tmp/$archive" -C "$tmp" shpyrd shpyrd-ctl 2>/dev/null || tar -xzf "$tmp/$archive" -C "$tmp" shpyrd

if [ -z "$INSTALL_DIR" ]; then
  if [ -w /usr/local/bin ]; then
    INSTALL_DIR=/usr/local/bin
  else
    INSTALL_DIR="$HOME/.local/bin"
  fi
fi
mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp/shpyrd" "$INSTALL_DIR/shpyrd"
installed="$INSTALL_DIR/shpyrd"
if [ -f "$tmp/shpyrd-ctl" ]; then
  install -m 0755 "$tmp/shpyrd-ctl" "$INSTALL_DIR/shpyrd-ctl"
  installed="$installed and $INSTALL_DIR/shpyrd-ctl"
fi
say "Installed $installed ($("$INSTALL_DIR/shpyrd" version 2>/dev/null || echo "$VERSION"))"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) say "Add $INSTALL_DIR to your PATH, for example: export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
esac
say "Next: shpyrd-ctl cluster create    (Docker required; https://shpyrd.io/docs/installation)"
