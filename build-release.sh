#!/bin/bash

# Script to build Stagehand binaries for multiple platforms
# Usage: ./build-release.sh [version]

set -e  # Exit on any error

VERSION=${1:-"latest"}
BUILD_DIR="build"
BINARY_PREFIX="godot-stagehand"

# Create build directory
mkdir -p $BUILD_DIR

echo "Building Stagehand v$VERSION..."

# Build for different architectures
echo "Building for Linux amd64..."
GOOS=linux GOARCH=amd64 go build -o "$BUILD_DIR/$BINARY_PREFIX-$VERSION-linux-amd64" .

echo "Building for macOS amd64..."
GOOS=darwin GOARCH=amd64 go build -o "$BUILD_DIR/$BINARY_PREFIX-$VERSION-darwin-amd64" .

echo "Building for macOS arm64..."
GOOS=darwin GOARCH=arm64 go build -o "$BUILD_DIR/$BINARY_PREFIX-$VERSION-darwin-arm64" .

echo "Building for Windows amd64..."
GOOS=windows GOARCH=amd64 go build -o "$BUILD_DIR/$BINARY_PREFIX-$VERSION-windows-amd64.exe" .

# Optional additional platforms
if [ "$TARGET_ALL_PLATFORMS" = "true" ]; then
    echo "Building for Linux arm64..."
    GOOS=linux GOARCH=arm64 go build -o "$BUILD_DIR/$BINARY_PREFIX-$VERSION-linux-arm64" .
    
    echo "Building for Linux 386..."
    GOOS=linux GOARCH=386 go build -o "$BUILD_DIR/$BINARY_PREFIX-$VERSION-linux-386" .
fi

echo ""
echo "Build completed! Binaries are in the $BUILD_DIR directory:"
ls -la $BUILD_DIR/

echo ""
echo "To create archives for distribution, run:"
echo "  tar -czvf ${BINARY_PREFIX}-${VERSION}-linux-amd64.tar.gz -C $BUILD_DIR ${BINARY_PREFIX}-${VERSION}-linux-amd64"
echo "  zip ${BINARY_PREFIX}-${VERSION}-windows-amd64.zip $BUILD_DIR/${BINARY_PREFIX}-${VERSION}-windows-amd64.exe"