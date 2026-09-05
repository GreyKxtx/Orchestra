#!/usr/bin/env bash
# Installs the latest (or a pinned) orchestra release for Linux/macOS.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/orchestra/orchestra/master/scripts/install.sh | bash
#   VERSION=v0.3.0 INSTALL_DIR=/usr/local/bin bash install.sh
#
# Downloads the matching orchestra_<version>_<target>.tar.gz from GitHub
# Releases, verifies it against the .sha256 published alongside it (release.yml
# writes both), and installs the orchestra binary. Requires curl and tar;
# uses sha256sum (Linux) or shasum (macOS), whichever is present.
set -euo pipefail

REPO="orchestra/orchestra"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

os="$(uname -s)"
case "$os" in
  Linux)  goos="linux" ;;
  Darwin) goos="darwin" ;;
  *) echo "install.sh: unsupported OS '$os' - see docs/examples or build from source (go install github.com/orchestra/orchestra/cmd/orchestra@latest)" >&2; exit 1 ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) goarch="amd64" ;;
  arm64|aarch64)
    if [ "$goos" = "linux" ]; then
      echo "install.sh: no linux-arm64 release yet - build from source (go install github.com/orchestra/orchestra/cmd/orchestra@latest)" >&2
      exit 1
    fi
    goarch="arm64"
    ;;
  *) echo "install.sh: unsupported arch '$arch'" >&2; exit 1 ;;
esac

target="${goos}-${goarch}"

if [ -z "${VERSION:-}" ]; then
  echo "Resolving latest release..."
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
  if [ -z "$VERSION" ]; then
    echo "install.sh: could not resolve the latest release - set VERSION=v0.x.y explicitly" >&2
    exit 1
  fi
fi

name="orchestra_${VERSION}_${target}"
archive="${name}.tar.gz"
base_url="https://github.com/${REPO}/releases/download/${VERSION}"

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

echo "Downloading ${archive} (${VERSION})..."
curl -fsSL -o "${work_dir}/${archive}" "${base_url}/${archive}"
curl -fsSL -o "${work_dir}/${archive}.sha256" "${base_url}/${archive}.sha256"

echo "Verifying checksum..."
(
  cd "$work_dir"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -c "${archive}.sha256"
  else
    shasum -a 256 -c "${archive}.sha256"
  fi
)

echo "Installing to ${INSTALL_DIR}..."
mkdir -p "$INSTALL_DIR"
tar -xzf "${work_dir}/${archive}" -C "$work_dir" orchestra
install -m 0755 "${work_dir}/orchestra" "${INSTALL_DIR}/orchestra"

echo "Installed $("${INSTALL_DIR}/orchestra" version)"
case ":$PATH:" in
  *":${INSTALL_DIR}:"*) ;;
  *) echo "Note: ${INSTALL_DIR} is not on your PATH. Add it, e.g.: export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
esac
