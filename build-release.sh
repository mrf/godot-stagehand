#!/bin/bash

# Script to build Stagehand binaries for multiple platforms
# Usage: ./build-release.sh [version]

set -e  # Exit on any error

VERSION=${1:-"latest"}
BUILD_DIR="build"

# Create build directory
mkdir -p "$BUILD_DIR"

# Update plugin.cfg version if a version was specified
if [ "$VERSION" != "latest" ]; then
    for cfg in addons/stagehand/plugin.cfg examples/minimal-game/addons/stagehand/plugin.cfg; do
        if [ -f "$cfg" ]; then
            sed -i "s/^version=\".*\"/version=\"$VERSION\"/" "$cfg"
            echo "Updated $cfg to v$VERSION"
        fi
    done
fi

echo "Building Stagehand v$VERSION..."

build_target() {
    local goos="$1"
    local goarch="$2"
    local asset_name="$3"
    echo "Building $asset_name..."
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -o "$BUILD_DIR/$asset_name" .
}

# Exact published asset matrix. Keep in sync with release.yml,
# RELEASE_CHECKLIST.md, README.md, and editor/release_assets.gd.
build_target linux   amd64 godot-stagehand-linux-amd64
build_target darwin  amd64 godot-stagehand-darwin-amd64
build_target darwin  arm64 godot-stagehand-darwin-arm64
build_target windows amd64 godot-stagehand-windows-amd64.exe

echo ""
echo "Build completed! Binaries are in the $BUILD_DIR directory:"
ls -la "$BUILD_DIR/"
