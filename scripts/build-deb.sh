#!/usr/bin/env bash
#
# Builds the talaia-agent Debian package into dist/.
#
# Usage: scripts/build-deb.sh [version] [arch]
#
#   version  defaults to the content of the VERSION file
#   arch     defaults to amd64; arm64 is also supported
set -eu

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

arch="${2:-amd64}"
case "$arch" in
    amd64 | arm64) ;;
    *)
        echo "error: unsupported architecture: $arch (use amd64 or arm64)" >&2
        exit 1
        ;;
esac

if ! command -v go >/dev/null 2>&1; then
    echo "error: go is required but was not found on PATH" >&2
    exit 1
fi
if ! command -v nfpm >/dev/null 2>&1; then
    echo "error: nfpm is required but was not found on PATH" >&2
    exit 1
fi

version="${1:-$(tr -d '[:space:]' < "$ROOT_DIR/VERSION")}"
version="$(printf '%s' "$version" | tr -d '[:space:]')"
if [ -z "$version" ]; then
    echo "error: version is empty" >&2
    exit 1
fi

dist="$ROOT_DIR/dist"
mkdir -p "$dist"

echo "building talaia-agent $version for linux/$arch"
(
    cd "$ROOT_DIR/agent"
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build \
        -trimpath \
        -ldflags "-s -w -X github.com/catlinux/Talaia/agent/internal/collect.version=$version" \
        -o "$dist/talaia-agent" ./cmd/talaia-agent
)

package="$dist/talaia-agent_${version}_${arch}.deb"
echo "packaging $package"
(
    # nfpm resolves the paths inside nfpm.yaml relative to the working
    # directory, so run it next to the configuration file.
    cd "$ROOT_DIR/packaging/agent"
    ARCH="$arch" VERSION="$version" nfpm package \
        --config nfpm.yaml \
        --packager deb \
        --target "$package"
)

echo "done: $package"
