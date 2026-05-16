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
    if [ -t 0 ]; then
        echo "Warning: Stagehand addon already exists at $COPY_DEST"
        read -p "Do you want to overwrite it? (y/N): " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            echo "Aborting."
            exit 1
        fi
    fi
    # Remove existing addon
    rm -rf "$COPY_DEST"
fi

echo "Copying Stagehand addon to target project..."
cp -r "$ADDON_SOURCE" "$COPY_DEST"

echo "Successfully copied Stagehand addon to $COPY_DEST"

# Auto-enable the plugin in project.godot
PROJECT_FILE="$TARGET_PROJECT/project.godot"
if [ -f "$PROJECT_FILE" ]; then
    PLUGIN_ENTRY="res://addons/stagehand/plugin.cfg"
    if grep -q "$PLUGIN_ENTRY" "$PROJECT_FILE"; then
        echo "Plugin already enabled in project.godot"
    elif grep -q '^\[editor_plugins\]' "$PROJECT_FILE"; then
        # Section exists — append to the enabled array
        if grep -q 'enabled=PackedStringArray()' "$PROJECT_FILE"; then
            # Empty array
            sed -i "s|enabled=PackedStringArray()|enabled=PackedStringArray(\"$PLUGIN_ENTRY\")|" "$PROJECT_FILE"
        elif grep -q 'enabled=PackedStringArray(' "$PROJECT_FILE"; then
            # Non-empty array — insert before closing paren
            sed -i "s|enabled=PackedStringArray(\(.*\))|enabled=PackedStringArray(\1, \"$PLUGIN_ENTRY\")|" "$PROJECT_FILE"
        fi
        echo "Enabled Stagehand plugin in project.godot"
    else
        # No [editor_plugins] section — append one
        printf '\n[editor_plugins]\n\nenabled=PackedStringArray("%s")\n' "$PLUGIN_ENTRY" >> "$PROJECT_FILE"
        echo "Added [editor_plugins] section and enabled Stagehand in project.godot"
    fi
    # Auto-register the autoload (required for runtime — plugin.gd only registers it in editor mode)
    AUTOLOAD_ENTRY='StagehandServer="*res://addons/stagehand/autoload/stagehand_server.gd"'
    if grep -q "StagehandServer" "$PROJECT_FILE"; then
        echo "Autoload already registered in project.godot"
    elif grep -q '^\[autoload\]' "$PROJECT_FILE"; then
        # Add after the [autoload] section header
        sed -i "/^\[autoload\]/a\\$AUTOLOAD_ENTRY" "$PROJECT_FILE"
        echo "Registered StagehandServer autoload in project.godot"
    else
        # No [autoload] section — append one
        printf '\n[autoload]\n\n%s\n' "$AUTOLOAD_ENTRY" >> "$PROJECT_FILE"
        echo "Added [autoload] section with StagehandServer in project.godot"
    fi
else
    echo "Warning: No project.godot found — enable the plugin manually in Project Settings > Plugins"
fi

echo ""
echo "Run your project with Stagehand active:"
echo ""
echo "  Linux / WSL:"
echo "    STAGEHAND_ENABLED=1 godot --path $TARGET_PROJECT"
echo "    godot --path $TARGET_PROJECT --stagehand"
echo ""
echo "  Windows (CMD):"
echo "    set STAGEHAND_ENABLED=1 && godot.exe --path \"$(wslpath -w "$TARGET_PROJECT" 2>/dev/null || echo "$TARGET_PROJECT")\""
echo "    godot.exe --path \"$(wslpath -w "$TARGET_PROJECT" 2>/dev/null || echo "$TARGET_PROJECT")\" --stagehand"
echo ""
echo "  Windows (PowerShell):"
echo "    \$env:STAGEHAND_ENABLED=\"1\"; & godot.exe --path \"$(wslpath -w "$TARGET_PROJECT" 2>/dev/null || echo "$TARGET_PROJECT")\""
echo ""
echo "You should see 'Stagehand: Server listening on port 26700' in the Godot output."