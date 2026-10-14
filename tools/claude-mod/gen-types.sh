#!/bin/sh
# Fetches the mod's TypeScript declarations from the pinned Claude Code CLI.
#
# As of 2.1.296 Claude Code writes .claude-plugin/types/ into a mod folder only
# when an interactive session hot-reloads it, never under `claude -p`. For a
# `claude -p` host its plugin-authoring docs say to type-check against the
# skill's own types/claude-code.d.ts, which the engine writes beside the skill
# as the skill loads (docs/design/claude-code-plugin.md, Q10). So this loads
# the skill with `claude -p /plugin-authoring` and copies that file out.
#
# The skill loads before the prompt is submitted. A UserPromptSubmit hook then
# holds the prompt back (exit 2), so no model request is made. The session
# also gets a private config dir, no credentials and a dead API endpoint, so a
# prompt that got past the hook could not reach a model either. This script
# must never spend anyone's usage.

set -eu

here=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
out="$here/.cc-types"
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT

mkdir -p "$here/.cc-config"
rm -rf "$out"

cat > "$scratch/hold.json" <<'EOF'
{
  "hooks": {
    "UserPromptSubmit": [
      { "hooks": [{ "type": "command", "command": "touch \"$GEN_TYPES_HELD\"; exit 2" }] }
    ]
  }
}
EOF

fail() {
    cat "$scratch/claude.log" >&2
    echo "gen-types: $1" >&2
    exit 1
}

if ! (
    cd "$scratch"
    env -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN -u CLAUDE_CODE_OAUTH_TOKEN \
        -u CLAUDE_CODE_USE_BEDROCK -u CLAUDE_CODE_USE_VERTEX -u CLAUDE_CODE_USE_FOUNDRY \
        CLAUDE_CONFIG_DIR="$here/.cc-config" \
        CLAUDE_CODE_TMPDIR="$scratch" \
        ANTHROPIC_BASE_URL=http://127.0.0.1:9 \
        DISABLE_AUTOUPDATER=1 \
        GEN_TYPES_HELD="$scratch/held" \
        "$here/node_modules/.bin/claude" -p /plugin-authoring --settings "$scratch/hold.json" \
        >"$scratch/claude.log" 2>&1
); then
    fail "claude -p /plugin-authoring failed"
fi
if [ ! -f "$scratch/held" ]; then
    fail "the UserPromptSubmit hook did not hold the prompt back"
fi
found=$(find "$scratch" -path '*/bundled-skills/*/plugin-authoring/types/claude-code.d.ts')
if [ -z "$found" ] || [ "$(printf '%s\n' "$found" | grep -c .)" -ne 1 ]; then
    fail "expected one plugin-authoring/types/claude-code.d.ts, found: ${found:-none}"
fi

mkdir -p "$out"
cp "$found" "$out/claude-code.d.ts"
echo "gen-types: wrote $out/claude-code.d.ts ($(head -n 1 "$out/claude-code.d.ts" | sed 's|^// ||'))"
