package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestCIScenarioSmokeRunsFullGodotTaggedSuite guards against the
// scenario-smoke job's `go test -tags=godot` invocation silently narrowing
// back to a single package or a `-run` filter. That exact narrowing (a
// trailing `.` instead of `./...`, plus `-run '^TestScenarioRunner'`) once
// left 26 of 28 real-Godot tests — including the entire MVP tool surface and
// the auth/bind-policy boundary — never gated by CI
// (godot-stagehand-ci-godot-tagged-suite-unwired-n7ib). A step that reappears
// with either narrowing must fail this test, not slip back in silently.
func TestCIScenarioSmokeRunsFullGodotTaggedSuite(t *testing.T) {
	repoRoot := addonInstallRepoRoot(t)

	workflow, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}

	const jobKey = "\n  scenario-smoke:\n"
	start := strings.Index(string(workflow), jobKey)
	if start < 0 {
		t.Fatal("scenario-smoke job not found in CI workflow")
	}
	body := string(workflow)[start+len(jobKey):]
	if end := strings.Index(body, "\n  build-matrix:"); end >= 0 {
		body = body[:end]
	}

	goTestRE := regexp.MustCompile(`go test -tags=godot[^\n]*`)
	matches := goTestRE.FindAllString(body, -1)
	if len(matches) != 1 {
		t.Fatalf("expected exactly one `go test -tags=godot` invocation in scenario-smoke, found %d: %v", len(matches), matches)
	}
	invocation := matches[0]

	if strings.Contains(invocation, "-run") {
		t.Errorf("scenario-smoke's godot-tagged test run must not use -run (it narrows the gate to a subset of tests): %q", invocation)
	}
	if !strings.Contains(invocation, "./...") {
		t.Errorf("scenario-smoke's godot-tagged test run must target ./... (the whole module), not a single package: %q", invocation)
	}
}
