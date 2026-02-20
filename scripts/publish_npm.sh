#!/usr/bin/env bash
#
# Publish Prysm CLI to npm. Copies prebuilt binaries into platform packages,
# updates versions, and publishes all 6 packages.
#
# Usage: scripts/publish_npm.sh <version> [--dry-run]
#
# Expects prebuilt binaries in dist/releases/<version>/ (built by release_artifacts.sh).
#
# Authentication: set NPM_TOKEN for CI or non-interactive publish:
#   export NPM_TOKEN="your-npm-auth-token"
# Get a token from https://www.npmjs.com/settings/~/tokens (Automation type for CI).

set -euo pipefail

usage() {
  cat <<EOF
Usage: $(basename "$0") <version> [--dry-run]

Examples:
  $(basename "$0") 0.0.7
  $(basename "$0") 0.0.7 --dry-run

Set NPM_TOKEN for CI/non-interactive publish:
  export NPM_TOKEN="your-npm-auth-token"
EOF
  exit 1
}

[[ $# -ge 1 ]] || usage

RAW_VERSION="$1"
VERSION="${RAW_VERSION#v}"
DRY_RUN=""

if [[ "${2:-}" == "--dry-run" ]]; then
  DRY_RUN="--dry-run"
  echo "Dry run mode — no packages will be published."
fi

if [[ -z "$VERSION" ]]; then
  echo "error: unable to determine version from input '$RAW_VERSION'" >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NPM_DIR="${ROOT_DIR}/npm"
DIST_DIR="${ROOT_DIR}/dist/releases/${VERSION}"

if [[ ! -d "$DIST_DIR" ]]; then
  echo "error: dist directory not found: $DIST_DIR" >&2
  echo "Run 'make publish VERSION=${VERSION}' first to build release artifacts." >&2
  exit 1
fi

# Use NPM_TOKEN for auth when set (CI or non-interactive)
if [[ -n "${NPM_TOKEN:-}" ]]; then
  NPMRC="$(mktemp)"
  echo "//registry.npmjs.org/:_authToken=${NPM_TOKEN}" > "$NPMRC"
  export NPM_CONFIG_USERCONFIG="$NPMRC"
  trap 'rm -f "$NPMRC"' EXIT
  echo "Using NPM_TOKEN for registry.npmjs.org"
else
  echo "NPM_TOKEN not set — using default npm auth (run 'npm login' or set NPM_TOKEN)"
fi
echo

# Map: npm package dir -> Go GOOS/GOARCH -> binary suffix
# Binary naming from release_artifacts.sh: prysm-cli-<VERSION>-<os>-<arch>[.exe]
PLATFORMS=(
  "cli-darwin-arm64:darwin:arm64:prysm"
  "cli-darwin-x64:darwin:amd64:prysm"
  "cli-linux-arm64:linux:arm64:prysm"
  "cli-linux-x64:linux:amd64:prysm"
  "cli-win32-x64:windows:amd64:prysm.exe"
)

echo "Publishing Prysm CLI v${VERSION} to npm"
echo

# Step 1: Copy binaries into platform packages and update versions
for entry in "${PLATFORMS[@]}"; do
  IFS=":" read -r pkg_dir goos goarch bin_name <<< "$entry"

  src_suffix=""
  [[ "$goos" == "windows" ]] && src_suffix=".exe"
  src="${DIST_DIR}/prysm-cli-${VERSION}-${goos}-${goarch}${src_suffix}"

  if [[ ! -f "$src" ]]; then
    echo "error: binary not found: $src" >&2
    exit 1
  fi

  dest="${NPM_DIR}/${pkg_dir}/bin/${bin_name}"
  cp "$src" "$dest"
  chmod 0755 "$dest"

  # Update version in package.json
  pkg_json="${NPM_DIR}/${pkg_dir}/package.json"
  tmp="$(mktemp)"
  sed "s/\"version\": \"[^\"]*\"/\"version\": \"${VERSION}\"/" "$pkg_json" > "$tmp"
  mv "$tmp" "$pkg_json"

  echo "  ${pkg_dir}: copied binary, set version to ${VERSION}"
done

# Step 2: Update main package version and optionalDependencies versions
main_pkg="${NPM_DIR}/prysm/package.json"
tmp="$(mktemp)"
sed "s/\"version\": \"[^\"]*\"/\"version\": \"${VERSION}\"/" "$main_pkg" |
  sed "s/\"@prysmsh\/cli-\([^\"]*\)\": \"[^\"]*\"/\"@prysmsh\/cli-\1\": \"${VERSION}\"/g" > "$tmp"
mv "$tmp" "$main_pkg"
echo "  prysm: set version to ${VERSION}"
echo

# Step 3: Publish platform packages first
echo "Publishing platform packages..."
for entry in "${PLATFORMS[@]}"; do
  IFS=":" read -r pkg_dir _ _ _ <<< "$entry"
  echo "  npm publish ${pkg_dir}..."
  npm publish "${NPM_DIR}/${pkg_dir}" --access public ${DRY_RUN}
done
echo

# Step 4: Publish main package
echo "Publishing main package..."
npm publish "${NPM_DIR}/prysm" --access public ${DRY_RUN}
echo

echo "Done! Published @prysmsh/cli@${VERSION}"
if [[ -z "$DRY_RUN" ]]; then
  echo "Test with: npx @prysmsh/cli@${VERSION} --version"
fi
