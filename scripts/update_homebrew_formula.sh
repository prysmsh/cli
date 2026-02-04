#!/usr/bin/env bash
#
# Update the Homebrew formula for prysm-cli to use artifacts from the prysm-cli repo.
# Usage: scripts/update_homebrew_formula.sh <version>

set -euo pipefail

REPO="${RELEASE_REPO:-prysmsh/cli}"
BASE_URL="https://github.com/${REPO}/releases/download"

if [[ $# -ne 1 ]]; then
  echo "Usage: $(basename "$0") <version>" >&2
  exit 1
fi

VERSION="${1#v}"
if [[ -z "$VERSION" ]]; then
  echo "error: version must not be empty" >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist/releases/${VERSION}"

if [[ ! -d "$DIST_DIR" ]]; then
  echo "error: distribution directory ${DIST_DIR} not found. Run scripts/release_artifacts.sh ${VERSION} first." >&2
  exit 1
fi

artifact_path() {
  local os="$1"
  local arch="$2"
  echo "${DIST_DIR}/prysm-cli-${VERSION}-${os}-${arch}.tar.gz"
}

sha256_for() {
  local file="$1"
  if [[ ! -f "$file" ]]; then
    echo "error: expected artifact ${file} not found" >&2
    exit 1
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
  else
    shasum -a 256 "$file" | awk '{print $1}'
  fi
}

ARM64_DARWIN_SHA=$(sha256_for "$(artifact_path darwin arm64)")
AMD64_DARWIN_SHA=$(sha256_for "$(artifact_path darwin amd64)")
ARM64_LINUX_SHA=$(sha256_for "$(artifact_path linux arm64)")
AMD64_LINUX_SHA=$(sha256_for "$(artifact_path linux amd64)")

# homebrew-tap is typically a sibling (../homebrew-tap) or submodule
FORMULA_DIR="${HOMEBREW_TAP_DIR:-${ROOT_DIR}/../homebrew-tap}/Formula"
mkdir -p "$FORMULA_DIR"
FORMULA_PATH="${FORMULA_DIR}/prysm-cli.rb"

cat >"$FORMULA_PATH" <<EOF
# frozen_string_literal: true

class PrysmCli < Formula
  desc "Prysm zero-trust infrastructure CLI"
  homepage "https://prysm.sh"
  version "${VERSION}"

  on_macos do
    if Hardware::CPU.arm?
      url "${BASE_URL}/v${VERSION}/prysm-cli-${VERSION}-darwin-arm64.tar.gz"
      sha256 "${ARM64_DARWIN_SHA}"
    else
      url "${BASE_URL}/v${VERSION}/prysm-cli-${VERSION}-darwin-amd64.tar.gz"
      sha256 "${AMD64_DARWIN_SHA}"
    end
  end

  on_linux do
    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      url "${BASE_URL}/v${VERSION}/prysm-cli-${VERSION}-linux-arm64.tar.gz"
      sha256 "${ARM64_LINUX_SHA}"
    else
      url "${BASE_URL}/v${VERSION}/prysm-cli-${VERSION}-linux-amd64.tar.gz"
      sha256 "${AMD64_LINUX_SHA}"
    end
  end

  def install
    bin.install "prysm"
    generate_completions_from_executable(bin/"prysm", "completion")
  end

  test do
    help_output = shell_output("\#{bin}/prysm --help")
    assert_match "Prysm zero-trust infrastructure CLI", help_output
  end
end
EOF

echo "Updated Homebrew formula at ${FORMULA_PATH}"
echo "URLs point to: github.com/${REPO}/releases"
