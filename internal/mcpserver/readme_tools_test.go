package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestReadmeToolTableMatchesRegisteredTools guards against README.md's
// "Available tools" table drifting from the tools actually registered on the
// server. It compares the live tool registry (via New()'s registerTools call)
// against every `godot_*` name documented in that section, in both
// directions.
func TestReadmeToolTableMatchesRegisteredTools(t *testing.T) {
	s := New()

	registered := make(map[string]bool)
	for name := range s.mcp.ListTools() {
		registered[name] = true
	}
	if len(registered) == 0 {
		t.Fatal("New() registered zero tools; test setup is broken")
	}

	repoRoot := filepath.Join("..", "..")
	content, err := os.ReadFile(filepath.Join(repoRoot, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}

	const heading = "## Available tools"
	section, err := readmeSection(string(content), heading)
	if err != nil {
		t.Fatal(err)
	}

	toolRefPattern := regexp.MustCompile("`(godot_[a-z_]+)`")
	documented := make(map[string]bool)
	for _, match := range toolRefPattern.FindAllStringSubmatch(section, -1) {
		documented[match[1]] = true
	}

	for name := range registered {
		if !documented[name] {
			t.Errorf("tool %q is registered on the server but not documented under %q in README.md", name, heading)
		}
	}
	for name := range documented {
		if !registered[name] {
			t.Errorf("README.md documents %q under %q, but no such tool is registered on the server", name, heading)
		}
	}
}

// readmeSection returns the text between a heading and the next top-level
// (##) heading, exclusive of both.
func readmeSection(content, heading string) (string, error) {
	start := strings.Index(content, heading)
	if start == -1 {
		return "", fmt.Errorf("README.md has no %q section", heading)
	}
	rest := content[start+len(heading):]
	if next := strings.Index(rest, "\n## "); next != -1 {
		return rest[:next], nil
	}
	return rest, nil
}
