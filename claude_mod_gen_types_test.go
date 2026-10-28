package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeClaudeForGenTypes stands in for the pinned CLI. It exits 90-94 when
// gen-types runs it any other way than with the hold mod, no credentials and
// a dead endpoint, then does what TYPE_TEST_BEHAVIOR says: what 2.1.302 does
// (the default), fail, write no declarations, or write them unheld.
const fakeClaudeForGenTypes = `#!/bin/sh
[ "$#" = 4 ] && [ "$1" = -p ] && [ "$2" = /plugin-authoring ] && [ "$3" = --plugin-dir ] || exit 90
grep -q "on('turn.step'" "$4/hooks/register.js" || exit 91
[ -z "${ANTHROPIC_API_KEY+x}${ANTHROPIC_AUTH_TOKEN+x}${CLAUDE_CODE_OAUTH_TOKEN+x}${CLAUDE_CODE_USE_BEDROCK+x}${CLAUDE_CODE_USE_VERTEX+x}${CLAUDE_CODE_USE_FOUNDRY+x}" ] || exit 92
[ "$ANTHROPIC_BASE_URL" = http://127.0.0.1:9 ] || exit 93
[ -n "$CLAUDE_CODE_TMPDIR" ] && [ -n "$CLAUDE_CONFIG_DIR" ] || exit 94
echo 'CLI diagnostic' >&2
[ "$TYPE_TEST_BEHAVIOR" != failure ] || exit 7
if [ "$TYPE_TEST_BEHAVIOR" != none ]; then
    d="$CLAUDE_CODE_TMPDIR/claude-1000/bundled-skills/9.9.9/0123/plugin-authoring/types"
    mkdir -p "$d"
    printf '// Written by Claude Code 9.9.9.\n// generated\n' > "$d/claude-code.d.ts"
fi
[ "$TYPE_TEST_BEHAVIOR" = unheld ] || echo 'gen-types: held'
`

func TestClaudeModGenTypes(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("get repository root: %v", err)
	}
	script, err := os.ReadFile(filepath.Join(repoRoot, "tools", "claude-mod", "gen-types.sh"))
	if err != nil {
		t.Fatalf("read gen-types.sh: %v", err)
	}

	const previous = "// previous declarations\n"
	for _, tc := range []struct {
		name, behavior, want string
		success              bool
	}{
		{"writes the skill's declarations", "", "gen-types: wrote", true},
		{"CLI failure keeps the previous declarations", "failure", "claude -p /plugin-authoring failed", false},
		{"no declarations keeps the previous ones", "none", "found: none", false},
		{"an unheld step keeps the previous declarations", "unheld", "hold mod did not answer", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			here := filepath.Join(t.TempDir(), "tools", "claude-mod")
			bin := filepath.Join(here, "node_modules", ".bin")
			out := filepath.Join(here, ".cc-types", "claude-code.d.ts")
			for _, dir := range []string{bin, filepath.Dir(out)} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatalf("create %s: %v", dir, err)
				}
			}
			if err := os.WriteFile(filepath.Join(here, "gen-types.sh"), script, 0o755); err != nil {
				t.Fatalf("copy gen-types.sh: %v", err)
			}
			if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(fakeClaudeForGenTypes), 0o755); err != nil {
				t.Fatalf("create fake claude: %v", err)
			}
			if err := os.WriteFile(out, []byte(previous), 0o644); err != nil {
				t.Fatalf("create previous declarations: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "sh", filepath.Join(here, "gen-types.sh"))
			cmd.Env = append(os.Environ(),
				"TYPE_TEST_BEHAVIOR="+tc.behavior,
				"ANTHROPIC_API_KEY=sk-test",
				"ANTHROPIC_AUTH_TOKEN=test",
				"CLAUDE_CODE_OAUTH_TOKEN=test",
				"CLAUDE_CODE_USE_BEDROCK=1",
				"CLAUDE_CODE_USE_VERTEX=1",
				"CLAUDE_CODE_USE_FOUNDRY=1",
			)
			output, err := cmd.CombinedOutput()
			if (err == nil) != tc.success {
				t.Fatalf("success=%v, err=%v\n%s", tc.success, err, output)
			}
			if !strings.Contains(string(output), tc.want) {
				t.Fatalf("want %q in output:\n%s", tc.want, output)
			}

			got, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("read declarations: %v", err)
			}
			if tc.success {
				if !strings.HasPrefix(string(got), "// Written by Claude Code 9.9.9.") {
					t.Fatalf("declarations not replaced with the skill's file:\n%s", got)
				}
				return
			}
			if string(got) != previous {
				t.Fatalf("a failed run changed the previous declarations:\n%s", got)
			}
			if !strings.Contains(string(output), "CLI diagnostic") {
				t.Fatalf("a failed run hid the CLI's output:\n%s", output)
			}
		})
	}
}
