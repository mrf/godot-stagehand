#!/bin/bash
#
# DEPRECATED: copy-addon.sh has been replaced by the Go-native setup command.
#
# Use instead:
#
#   godot-stagehand setup /path/to/your/godot/project
#
# The setup command copies the addon, enables the plugin, registers the
# StagehandServer autoload (idempotently), and prints the MCP client config
# snippet and run command — no bash dependency required. Add --force to
# overwrite an existing addon installation.
#
# This shim forwards to the Go-native command so existing muscle memory keeps
# working.

set -e

echo "⚠  copy-addon.sh is deprecated. Use: godot-stagehand setup <project_path>" >&2

if [ $# -eq 0 ]; then
    echo "Usage: godot-stagehand setup <path-to-godot-project>" >&2
    echo "       (or, from this repo:  go run . setup <path-to-godot-project>)" >&2
    exit 1
fi

# Forward to the Go-native command. Prefer an installed binary; otherwise build
# from the repo via `go run`.
if command -v godot-stagehand >/dev/null 2>&1; then
    exec godot-stagehand setup "$@"
elif command -v go >/dev/null 2>&1; then
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    exec go run "$SCRIPT_DIR" setup "$@"
else
    echo "Error: neither 'godot-stagehand' nor 'go' found on PATH." >&2
    echo "Install the godot-stagehand binary and run: godot-stagehand setup $*" >&2
    exit 1
fi
