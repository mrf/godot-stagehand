//go:build godot

package mcpserver

import (
	"os"
	"path/filepath"
	"testing"
)

// prepareMCPGodotProject copies the standard test project plus the canonical
// stagehand addon into a fresh temp dir. Real-Godot tests that launch more
// than one instance call this once per instance so concurrent launches never
// share a project directory unless a test is deliberately exercising
// same-project contention.
func prepareMCPGodotProject(t *testing.T, repoRoot string) string {
	t.Helper()
	projectDir := filepath.Join(t.TempDir(), "project")
	if err := os.CopyFS(projectDir, os.DirFS(filepath.Join(repoRoot, "testdata", "test_project"))); err != nil {
		t.Fatalf("copy test project: %v", err)
	}
	addonDir := filepath.Join(projectDir, "addons", "stagehand")
	if err := os.RemoveAll(addonDir); err != nil {
		t.Fatalf("remove fixture addon copy: %v", err)
	}
	if err := os.CopyFS(addonDir, os.DirFS(filepath.Join(repoRoot, "addons", "stagehand"))); err != nil {
		t.Fatalf("copy canonical addon: %v", err)
	}
	return projectDir
}
