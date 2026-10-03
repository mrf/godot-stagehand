#!/bin/sh
# Generates the mod's TypeScript declarations for the pinned Claude Code CLI.
#
# Claude Code writes them into integrations/claude-code/.claude-plugin/types/
# only when a session loads the plugin; `claude plugin validate` and `claude
# plugin test` do not (docs/design/claude-code-plugin.md, Q10). So this runs
# the mod's own command in a non-interactive session, which needs no model.
#
# The session gets a private config dir, no credentials and a dead API
# endpoint. If the mod ever failed to load, /stagehand-view would go to a
# model instead, and this script must never spend anyone's usage.

set -eu

here=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
plugin=$(CDPATH='' cd -- "$here/../../integrations/claude-code" && pwd)
types="$plugin/.claude-plugin/types"

mkdir -p "$here/.cc-config"
rm -rf "$types"

env -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN -u CLAUDE_CODE_OAUTH_TOKEN \
    CLAUDE_CONFIG_DIR="$here/.cc-config" \
    ANTHROPIC_BASE_URL=http://127.0.0.1:9 \
    STAGEHAND_RELEASE_BASE=http://127.0.0.1:9 \
    DISABLE_AUTOUPDATER=1 \
    "$here/node_modules/.bin/claude" -p /stagehand-view --plugin-dir "$plugin" >/dev/null

if [ ! -f "$types/claude-code/index.d.ts" ]; then
    echo "gen-types: Claude Code wrote no declarations to $types; did the mod load?" >&2
    exit 1
fi
echo "gen-types: wrote $types"
