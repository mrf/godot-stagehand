#!/bin/bash

# Script to copy Stagehand addon to another Godot project
# Usage: ./copy-addon.sh /path/to/target/godot/project

set -e  # Exit on any error

if [ $# -eq 0 ]; then
    echo "Usage: $0 <path-to-godot-project>"
    echo "Example: $0 /home/user/my-godot-project/"
    exit 1
fi

TARGET_PROJECT="$1"
ADDON_SOURCE="./addons/stagehand"

echo "Checking if source addon exists..."
if [ ! -d "$ADDON_SOURCE" ]; then
    echo "Error: Source addon directory '$ADDON_SOURCE' does not exist."
    exit 1
fi

echo "Checking if target project exists..."
if [ ! -d "$TARGET_PROJECT" ]; then
    echo "Error: Target project directory '$TARGET_PROJECT' does not exist."
    exit 1
fi

# Create addons directory in target project if it doesn't exist
TARGET_ADDONS_DIR="$TARGET_PROJECT/addons"
if [ ! -d "$TARGET_ADDONS_DIR" ]; then
    echo "Creating addons directory in target project..."
    mkdir -p "$TARGET_ADDONS_DIR"
fi

# Copy the stagehand addon to the target project
COPY_DEST="$TARGET_ADDONS_DIR/stagehand"

if [ -d "$COPY_DEST" ]; then
    echo "Warning: Stagehand addon already exists at $COPY_DEST"
    read -p "Do you want to overwrite it? (y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Aborting."
        exit 1
    fi
    # Remove existing addon
    rm -rf "$COPY_DEST"
fi

echo "Copying Stagehand addon to target project..."
cp -r "$ADDON_SOURCE" "$COPY_DEST"

echo "Successfully copied Stagehand addon to $COPY_DEST"
echo ""
echo "Next steps:"
echo "1. Open your Godot project"
echo "2. Go to Project > Project Settings > Plugins "
echo "3. Enable the 'Stagehand' plugin"
echo ""
echo "Remember to enable the plugin by running Godot with:"
echo "  STAGEHAND_ENABLED=1 godot --path $TARGET_PROJECT"
echo "Or using the command line option:"
echo "  godot --path $TARGET_PROJECT --stagehand"