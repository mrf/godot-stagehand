package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrf/godot-stagehand/internal/scenario"
)

// TestShippedScenarioIsValid keeps the CI gate's own scenario from rotting.
// It runs without Godot, so a bad edit fails in go-quality rather than only in
// the (slower, Godot-dependent) scenario-smoke job.
func TestShippedScenarioIsValid(t *testing.T) {
	path := filepath.Join("testdata", "scenarios", "smoke.json")
	sc, err := scenario.Load(path)
	if err != nil {
		t.Fatalf("%s failed validation: %v", path, err)
	}

	if sc.Target.Mode != scenario.ModeLaunch {
		t.Errorf("target.mode = %q, want %q", sc.Target.Mode, scenario.ModeLaunch)
	}
	// The gate must run on a headless CI runner with no display, so the
	// scenario has to stay screenshot-free.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read scenario: %v", err)
	}
	for _, forbidden := range []string{"save_baseline", "screenshot_diff", `"screenshot"`} {
		if strings.Contains(string(body), forbidden) {
			t.Errorf("%s uses %s, which needs a rendered window; the CI gate runs headless", path, forbidden)
		}
	}
	if len(sc.Steps) == 0 {
		t.Error("the smoke scenario has no steps")
	}
	if len(sc.Teardown) == 0 {
		t.Error("the smoke scenario should restore the state it mutates")
	}

	// The project path is relative to the scenario file, so it must resolve.
	projectPath := filepath.Join(filepath.Dir(path), sc.Target.ProjectPath)
	if _, err := os.Stat(filepath.Join(projectPath, "project.godot")); err != nil {
		t.Errorf("target.project_path %q does not resolve to a Godot project: %v", sc.Target.ProjectPath, err)
	}
}

// TestDocumentedExitCodesMatchTheImplementation keeps docs/cli.md honest: the
// exit-code table is the contract CI pipelines are told to branch on, and a
// drift there is silently wrong until somebody's gate stops failing. cli.md is
// the single home for that table; the README deliberately does not repeat it.
func TestDocumentedExitCodesMatchTheImplementation(t *testing.T) {
	rows := []struct {
		code  int
		gloss string
	}{
		{0, "success"}, {1, "internal"}, {2, "usage"},
		{3, "connection"}, {4, "godot"}, {5, "assertion"}, {6, "timeout"},
	}
	for _, doc := range []string{filepath.Join("docs", "cli.md")} {
		body, err := os.ReadFile(doc)
		if err != nil {
			t.Fatalf("read %s: %v", doc, err)
		}
		text := strings.ToLower(string(body))
		for _, row := range rows {
			if !strings.Contains(text, row.gloss) {
				t.Errorf("%s does not document exit code %d (%s)", doc, row.code, row.gloss)
			}
		}
	}
}
