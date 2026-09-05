#!/usr/bin/env bash
# Generates a Homebrew formula, a Scoop manifest, and a winget manifest set
# from a release's published archives + .sha256 files, so brew/scoop/winget
# installs work straight off a GitHub release without waiting on a separate
# tap/bucket repo to exist (all three tools can install from a manifest URL
# or local path directly; a tap/bucket only shortens the install command).
#
# Usage:
#   scripts/gen-packaging.sh <version> <dist-dir> <out-dir>
#
# <dist-dir> must contain orchestra_<version>_<target>.(tar.gz|zip).sha256
# for darwin-arm64, darwin-amd64, linux-amd64, windows-amd64 - exactly what
# release.yml's build job already produces and the publish job downloads.
set -euo pipefail

VERSION="${1:?usage: gen-packaging.sh <version> <dist-dir> <out-dir>}"
DIST_DIR="${2:?usage: gen-packaging.sh <version> <dist-dir> <out-dir>}"
OUT_DIR="${3:?usage: gen-packaging.sh <version> <dist-dir> <out-dir>}"

REPO="GreyKxtx/Orchestra"
BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
BARE_VERSION="${VERSION#v}"
PKG_ID="GreyKxtx.Orchestra"

sha_of() {
  local file="$1"
  local sha_file="${DIST_DIR}/${file}.sha256"
  [ -f "$sha_file" ] || { echo "gen-packaging.sh: missing ${sha_file}" >&2; exit 1; }
  awk '{print $1}' "$sha_file"
}

darwin_arm64="orchestra_${VERSION}_darwin-arm64.tar.gz"
darwin_amd64="orchestra_${VERSION}_darwin-amd64.tar.gz"
linux_amd64="orchestra_${VERSION}_linux-amd64.tar.gz"
windows_amd64="orchestra_${VERSION}_windows-amd64.zip"

sha_darwin_arm64="$(sha_of "$darwin_arm64")"
sha_darwin_amd64="$(sha_of "$darwin_amd64")"
sha_linux_amd64="$(sha_of "$linux_amd64")"
sha_windows_amd64="$(sha_of "$windows_amd64")"

winget_dir="${OUT_DIR}/winget/manifests/g/GreyKxtx/Orchestra/${BARE_VERSION}"
mkdir -p "$winget_dir"

# --- Homebrew formula --------------------------------------------------------
# Standalone-installable today: brew install --formula <raw-url-to-orchestra.rb>
# `brew install orchestra` needs a tap repo (e.g. GreyKxtx/homebrew-orchestra)
# with this file at Formula/orchestra.rb - not created by this script.
cat > "${OUT_DIR}/orchestra.rb" <<EOF
class Orchestra < Formula
  desc "Local AI coding assistant - LLM agent loop over JSON-RPC 2.0 stdio"
  homepage "https://github.com/${REPO}"
  version "${BARE_VERSION}"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "${BASE_URL}/${darwin_arm64}"
      sha256 "${sha_darwin_arm64}"
    else
      url "${BASE_URL}/${darwin_amd64}"
      sha256 "${sha_darwin_amd64}"
    end
  end

  on_linux do
    url "${BASE_URL}/${linux_amd64}"
    sha256 "${sha_linux_amd64}"
  end

  def install
    bin.install "orchestra"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/orchestra version")
  end
end
EOF

# --- Scoop manifest -----------------------------------------------------------
# Standalone-installable today: scoop install <raw-url-to-orchestra.json>
# `scoop install orchestra` needs a bucket repo added via `scoop bucket add`.
cat > "${OUT_DIR}/orchestra.json" <<EOF
{
    "version": "${BARE_VERSION}",
    "description": "Local AI coding assistant - LLM agent loop over JSON-RPC 2.0 stdio",
    "homepage": "https://github.com/${REPO}",
    "license": "MIT",
    "url": "${BASE_URL}/${windows_amd64}",
    "hash": "sha256:${sha_windows_amd64}",
    "bin": "orchestra.exe",
    "checkver": {
        "github": "https://github.com/${REPO}"
    },
    "autoupdate": {
        "url": "https://github.com/${REPO}/releases/download/v\$version/orchestra_v\$version_windows-amd64.zip"
    }
}
EOF

# --- winget manifest set --------------------------------------------------
# Local test: winget install --manifest <winget_dir>
# Public listing needs a PR against microsoft/winget-pkgs - not done here,
# that's a submission to a third-party repo and stays a manual step.
cat > "${winget_dir}/${PKG_ID}.yaml" <<EOF
PackageIdentifier: ${PKG_ID}
PackageVersion: ${BARE_VERSION}
DefaultLocale: en-US
ManifestType: version
ManifestVersion: 1.6.0
EOF

cat > "${winget_dir}/${PKG_ID}.installer.yaml" <<EOF
PackageIdentifier: ${PKG_ID}
PackageVersion: ${BARE_VERSION}
InstallerType: zip
NestedInstallerType: portable
NestedInstallerFiles:
  - RelativeFilePath: orchestra.exe
    PortableCommandAlias: orchestra
Installers:
  - Architecture: x64
    InstallerUrl: ${BASE_URL}/${windows_amd64}
    InstallerSha256: ${sha_windows_amd64}
ManifestType: installer
ManifestVersion: 1.6.0
EOF

cat > "${winget_dir}/${PKG_ID}.locale.en-US.yaml" <<EOF
PackageIdentifier: ${PKG_ID}
PackageVersion: ${BARE_VERSION}
PackageLocale: en-US
Publisher: Andrey Korsun
PackageName: Orchestra
PackageUrl: https://github.com/${REPO}
License: MIT
ShortDescription: Local AI coding assistant - LLM agent loop over JSON-RPC 2.0 stdio
ManifestType: defaultLocale
ManifestVersion: 1.6.0
EOF

echo "Wrote ${OUT_DIR}/orchestra.rb"
echo "Wrote ${OUT_DIR}/orchestra.json"
echo "Wrote ${winget_dir}/${PKG_ID}.yaml (+.installer.yaml, +.locale.en-US.yaml)"
