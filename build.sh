#!/bin/bash

# Simple build script to build Stagehand for the current platform
# Usage: ./build.sh [output-name]

OUTPUT_NAME=${1:-"godot-stagehand"}

echo "Building Stagehand for current platform..."

go build -o "$OUTPUT_NAME" .

if [ $? -eq 0 ]; then
    echo "Build successful! Created: $OUTPUT_NAME"
    ls -la "$OUTPUT_NAME"
else
    echo "Build failed!"
    exit 1
fi