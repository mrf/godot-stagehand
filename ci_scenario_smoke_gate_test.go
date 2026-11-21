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
	invocation := scenarioSmokeGodotTestInvocation(t, scenarioSmokeJobBody(t))

	_, goTestArgs, _ := strings.Cut(invocation, "go test")
	if regexp.MustCompile(`\s-run\b`).MatchString(goTestArgs) {
		t.Errorf("scenario-smoke's godot-tagged test run must not use -run (it narrows the gate to a subset of tests): %q", invocation)
	}
	if !strings.Contains(invocation, "./...") {
		t.Errorf("scenario-smoke's godot-tagged test run must target ./... (the whole module), not a single package: %q", invocation)
	}
}

// TestCIScenarioSmokeGodotSuiteHasDisplay guards the godot-tagged suite's
// display. Some of its tests are headed (hostcompat's
// TestTemplateRunsGreenAgainstTestProject takes a screenshot), and on a
// display-less runner Godot exits with "Unable to create DisplayServer, all
// display drivers failed" before the test can connect. ubuntu-latest does not
// guarantee Xvfb, so the job must install it as well as run under it.
func TestCIScenarioSmokeGodotSuiteHasDisplay(t *testing.T) {
	body := scenarioSmokeJobBody(t)
	invocation := scenarioSmokeGodotTestInvocation(t, body)

	if !regexp.MustCompile(`(?:^|\s)xvfb-run -a go test -tags=godot`).MatchString(invocation) {
		t.Errorf("scenario-smoke's godot-tagged test run must run under `xvfb-run -a` (it contains headed tests): %q", invocation)
	}
	if !regexp.MustCompile(`apt-get install[^\n]*\bxvfb\b`).MatchString(body) {
		t.Error("scenario-smoke must install the xvfb package explicitly; ubuntu-latest does not guarantee it")
	}
}

// scenarioSmokeJobBody returns the scenario-smoke job's YAML, up to the next job.
func scenarioSmokeJobBody(t *testing.T) string {
	t.Helper()
	workflow, err := os.ReadFile(filepath.Join(addonInstallRepoRoot(t), ".github", "workflows", "ci.yml"))
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
	return body
}

// scenarioSmokeGodotTestInvocation returns the job's single godot-tagged
// `go test` line, including any command prefix such as xvfb-run.
func scenarioSmokeGodotTestInvocation(t *testing.T, body string) string {
	t.Helper()
	goTestRE := regexp.MustCompile(`[^\n]*go test -tags=godot[^\n]*`)
	matches := goTestRE.FindAllString(body, -1)
	if len(matches) != 1 {
		t.Fatalf("expected exactly one `go test -tags=godot` invocation in scenario-smoke, found %d: %v", len(matches), matches)
	}
	return matches[0]
}
