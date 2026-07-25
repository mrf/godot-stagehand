package mcpserver

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestSkillFrontmatterParses guards skills/stagehand.md's YAML frontmatter
// against silent corruption (e.g. a stray edit that breaks the delimiters or
// drops a required key), since nothing else in the build validates it.
func TestSkillFrontmatterParses(t *testing.T) {
	content := readSkillFile(t)

	if !strings.HasPrefix(content, "---\n") {
		t.Fatal("skills/stagehand.md must start with a `---` frontmatter delimiter")
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end == -1 {
		t.Fatal("skills/stagehand.md frontmatter has no closing `---` delimiter")
	}
	frontmatter := rest[:end]

	for _, key := range []string{"name:", "description:"} {
		if !strings.Contains(frontmatter, "\n"+key) && !strings.HasPrefix(frontmatter, key) {
			t.Errorf("skills/stagehand.md frontmatter is missing required key %q", key)
		}
	}
}

// TestSkillToolReferencesAreRegistered guards against skills/stagehand.md
// rotting silently as MCP tools are renamed or removed: every `#### `godot_x``
// heading under "## Tool Reference" must name a tool actually registered on
// the server. Unlike TestReadmeToolTableMatchesRegisteredTools, this is
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
		t.Fatalf("skills/stagehand.md: %v", err)
	}

	toolHeadingPattern := regexp.MustCompile("(?m)^#### `(godot_[a-z_]+)`\\s*$")
	matches := toolHeadingPattern.FindAllStringSubmatch(section, -1)
	if len(matches) == 0 {
		t.Fatal("skills/stagehand.md's Tool Reference section has no `#### `godot_x`` headings; pattern may be stale")
	}
	for _, match := range matches {
		name := match[1]
		if !registered[name] {
			t.Errorf("skills/stagehand.md documents %q as a tool, but no such tool is registered on the server", name)
		}
	}
}

func readSkillFile(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "skills", "stagehand.md"))
	if err != nil {
		t.Fatalf("read skills/stagehand.md: %v", err)
	}
	return string(content)
}
