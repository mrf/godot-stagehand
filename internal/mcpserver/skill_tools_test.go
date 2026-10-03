package mcpserver

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// skillPath is the one canonical copy of the agent skill. It lives inside the
// Claude Code plugin because plugin skills must use the <name>/SKILL.md layout
// under the plugin root (docs/design/claude-code-plugin.md, D5).
const skillPath = "integrations/claude-code/skills/stagehand/SKILL.md"

// TestSkillFrontmatterParses guards the skill's YAML frontmatter against
// silent corruption (e.g. a stray edit that breaks the delimiters or drops a
// required key), since nothing in `go test` otherwise validates it.
func TestSkillFrontmatterParses(t *testing.T) {
	content := readSkillFile(t)

	if !strings.HasPrefix(content, "---\n") {
		t.Fatalf("%s must start with a `---` frontmatter delimiter", skillPath)
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end == -1 {
		t.Fatalf("%s frontmatter has no closing `---` delimiter", skillPath)
	}
	frontmatter := rest[:end]

	for _, key := range []string{"name:", "description:"} {
		if !strings.Contains(frontmatter, "\n"+key) && !strings.HasPrefix(frontmatter, key) {
			t.Errorf("%s frontmatter is missing required key %q", skillPath, key)
		}
	}
}

// TestSkillHasOneCanonicalCopy fails if the pre-plugin skills/stagehand.md
// comes back next to the plugin skill: two copies drift, and only one of them
// is guarded by the tests in this file.
func TestSkillHasOneCanonicalCopy(t *testing.T) {
	legacy := filepath.Join("..", "..", "skills", "stagehand.md")
	if _, err := os.Stat(legacy); err == nil {
		t.Errorf("skills/stagehand.md exists again; the only copy of the skill is %s", skillPath)
	}
}

// TestSkillToolReferencesAreRegistered guards against the skill rotting
// silently as MCP tools are renamed or removed: every level-4 heading of the
// form "#### `godot_x`" under "## Tool Reference" must name a tool actually
// registered on the server. Unlike TestReadmeToolTableMatchesRegisteredTools, this is
// one-directional — the skill is a curated walkthrough, not a full reference,
// so it need not document every registered tool.
func TestSkillToolReferencesAreRegistered(t *testing.T) {
	s := New()
	registered := make(map[string]bool)
	for name := range s.mcp.ListTools() {
		registered[name] = true
	}
	if len(registered) == 0 {
		t.Fatal("New() registered zero tools; test setup is broken")
	}

	content := readSkillFile(t)
	section, err := markdownSection(content, "## Tool Reference")
	if err != nil {
		t.Fatalf("%s: %v", skillPath, err)
	}

	toolHeadingPattern := regexp.MustCompile("(?m)^#### `(godot_[a-z_]+)`\\s*$")
	matches := toolHeadingPattern.FindAllStringSubmatch(section, -1)
	if len(matches) == 0 {
		t.Fatalf("%s's Tool Reference section has no `#### `godot_x`` headings; pattern may be stale", skillPath)
	}
	for _, match := range matches {
		name := match[1]
		if !registered[name] {
			t.Errorf("%s documents %q as a tool, but no such tool is registered on the server", skillPath, name)
		}
	}
}

func readSkillFile(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(skillPath)))
	if err != nil {
		t.Fatalf("read %s: %v", skillPath, err)
	}
	return string(content)
}
