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
	assertRunsUnderXvfb(t, "scenario-smoke", body, scenarioSmokeGodotTestInvocation(t, body), "go test -tags=godot")
}

// TestReportCardTestsCheckHasDisplay is the report card's counterpart: its
// tests check runs the same godot-tagged suite (reportcard/config.toml's
// extra_args), so the generator must run under a display too, or the headed
// test fails and coverage_percent comes back null.
func TestReportCardTestsCheckHasDisplay(t *testing.T) {
	body := workflowJobBody(t, "report-card.yml", "analyze", "publish-badges")
	matches := regexp.MustCompile(`[^\n]*python3 reportcard/generate\.py[^\n]*`).FindAllString(body, -1)
	if len(matches) != 1 {
		t.Fatalf("expected exactly one reportcard/generate.py invocation in the report card's analyze job, found %d: %v", len(matches), matches)
	}
	assertRunsUnderXvfb(t, "report card analyze", body, matches[0], "python3 reportcard/generate.py")
}

// assertRunsUnderXvfb fails unless invocation runs command under
// `xvfb-run -a` and the job body installs the xvfb package.
func assertRunsUnderXvfb(t *testing.T, job, body, invocation, command string) {
	t.Helper()
	if !regexp.MustCompile(`(?:^|\s)xvfb-run -a ` + regexp.QuoteMeta(command)).MatchString(invocation) {
		t.Errorf("%s's %q must run under `xvfb-run -a` (the godot-tagged suite contains headed tests): %q", job, command, invocation)
	}
	if !regexp.MustCompile(`apt-get install[^\n]*\bxvfb\b`).MatchString(body) {
		t.Errorf("%s must install the xvfb package explicitly; ubuntu-latest does not guarantee it", job)
	}
}

// scenarioSmokeJobBody returns the scenario-smoke job's YAML, up to the next job.
func scenarioSmokeJobBody(t *testing.T) string {
	t.Helper()
	return workflowJobBody(t, "ci.yml", "scenario-smoke", "build-matrix")
}

// workflowJobBody returns job's YAML from .github/workflows/<file>, up to
// nextJob (or the end of the file if nextJob is absent).
func workflowJobBody(t *testing.T, file, job, nextJob string) string {
	t.Helper()
	workflow, err := os.ReadFile(filepath.Join(addonInstallRepoRoot(t), ".github", "workflows", file))
	if err != nil {
		t.Fatalf("read workflow %s: %v", file, err)
	}

	jobKey := "\n  " + job + ":\n"
	start := strings.Index(string(workflow), jobKey)
	if start < 0 {
		t.Fatalf("%s job not found in %s", job, file)
	}
	body := string(workflow)[start+len(jobKey):]
	if end := strings.Index(body, "\n  "+nextJob+":"); end >= 0 {
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
