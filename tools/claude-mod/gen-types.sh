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
# As of 2.1.302 the skill writes its files at skill.prompt, after the
# UserPromptSubmit hooks have run, so the prompt has to get that far. A
# throwaway mod then answers the model step itself (turn.step, without
# next()), so no model request is made. The session also gets a private config
# dir, no credentials and a dead API endpoint, so a step that got past the mod
# could not reach a model either. This script must never spend anyone's usage.
#
# The previous declarations stay in place unless a new file is found.

set -eu

here=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
out="$here/.cc-types"
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT

mkdir -p "$here/.cc-config" "$scratch/hold/.claude-plugin" "$scratch/hold/hooks"

cat > "$scratch/hold/.claude-plugin/plugin.json" <<'EOF'
{ "name": "gen-types-hold", "version": "0.0.0", "description": "Answers the model step itself, so gen-types makes no model request" }
EOF
cat > "$scratch/hold/hooks/hooks.json" <<'EOF'
{ "description": "gen-types hold", "modules": ["./register.js"] }
EOF
cat > "$scratch/hold/hooks/register.js" <<'EOF'
const answer = 'gen-types: held'

export function register(on) {
  on('turn.step', async function* ($, e) {
    yield { kind: 'text', index: 0, text: answer }
    yield { kind: 'stop', stopReason: 'end_turn', usage: null }
    return { turnId: e.turnId, index: e.index, answer, toolUses: [], stopReason: 'end_turn', usage: null }
  })
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
        "$here/node_modules/.bin/claude" -p /plugin-authoring --plugin-dir "$scratch/hold" \
        </dev/null >"$scratch/claude.log" 2>&1
); then
    fail "claude -p /plugin-authoring failed"
fi
if ! grep -Fqx 'gen-types: held' "$scratch/claude.log"; then
    fail "the hold mod did not answer the model step"
fi
found=$(find "$scratch" -path '*/bundled-skills/*/plugin-authoring/types/claude-code.d.ts')
if [ -z "$found" ] || [ "$(printf '%s\n' "$found" | grep -c .)" -ne 1 ]; then
    fail "expected one plugin-authoring/types/claude-code.d.ts, found: ${found:-none}"
fi

mkdir -p "$out"
cp "$found" "$out/claude-code.d.ts.new"
mv -f "$out/claude-code.d.ts.new" "$out/claude-code.d.ts"
echo "gen-types: wrote $out/claude-code.d.ts ($(head -n 1 "$out/claude-code.d.ts" | sed 's|^// ||'))"
