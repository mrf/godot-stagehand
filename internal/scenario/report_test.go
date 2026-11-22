package scenario

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSummaryDoesNotDoubleTheWordStep reproduces godot-stagehand-1lwq: Phase
// is literally "step" for the main phase, so formatting "%s step %d" printed
// "failed at step step 1" instead of "failed at step 1".
func TestSummaryDoesNotDoubleTheWordStep(t *testing.T) {
	idx := 1
	r := &Report{
		Name:   "smoke",
		Status: StatusFailed,
		Failure: &Failure{
			StepIndex: &idx,
			Phase:     "step",
			Kind:      KindAssertion,
			Message:   "boom",
		},
	}
	summary := r.Summary()
	if want := "failed at step 1 (assertion): boom"; !strings.Contains(summary, want) {
		t.Fatalf("Summary() = %q, want it to contain %q", summary, want)
	}
}

func TestSummaryLabelsTeardownStepCorrectly(t *testing.T) {
	idx := 0
	r := &Report{
		Name:   "smoke",
		Status: StatusFailed,
		Failure: &Failure{
			StepIndex: &idx,
			Phase:     "teardown",
			Kind:      KindAssertion,
			Message:   "boom",
		},
	}
	summary := r.Summary()
	if want := "failed at teardown step 0 (assertion): boom"; !strings.Contains(summary, want) {
		t.Fatalf("Summary() = %q, want it to contain %q", summary, want)
	}
}

// TestSummaryOmitsFakeStepIndexForPreStepFailure reproduces
// godot-stagehand-07v1: a connect failure happens before any step runs, so
// there is no step to index. Location() must not surface the -1 sentinel.
func TestSummaryOmitsFakeStepIndexForPreStepFailure(t *testing.T) {
	r := &Report{
		Name:   "unreachable",
		Status: StatusFailed,
		Failure: &Failure{
			Phase:   "connect",
			Kind:    KindConnection,
			Message: "dial tcp 127.0.0.1:1: connect: connection refused",
		},
	}
	summary := r.Summary()
	if want := "failed at connect phase (connection):"; !strings.Contains(summary, want) {
		t.Fatalf("Summary() = %q, want it to contain %q", summary, want)
	}
	if strings.Contains(summary, "-1") {
		t.Errorf("Summary() = %q, must not leak the -1 step sentinel", summary)
	}
}

// A scenario with 9 steps + 1 teardown step must report 9 in the step tally,
// not 10 — teardown is cleanup, not an assertion, and CI dashboards reading
// the summary line expect it to agree with report.json's Steps array length.
func nineStepsOneTeardown() *Report {
	steps := make([]StepResult, 9)
	for i := range steps {
		steps[i] = StepResult{Index: i, Name: "step", Action: "click", Status: StatusPassed}
	}
	return &Report{
		Name:   "smoke",
		Status: StatusPassed,
		Steps:  steps,
		Teardown: []StepResult{
			{Index: 0, Name: "cleanup", Action: "change_scene", Status: StatusPassed},
		},
	}
}

func TestSummaryExcludesTeardownFromStepCounts(t *testing.T) {
	r := nineStepsOneTeardown()
	summary := r.Summary()
	if !strings.Contains(summary, "9 passed, 0 failed, 0 skipped") {
		t.Errorf("summary = %q, want step tally of 9 passed, not 10", summary)
	}
}

func TestSummarySurfacesTeardownFailure(t *testing.T) {
	r := nineStepsOneTeardown()
	r.Teardown[0].Status = StatusFailed
	r.Teardown[0].Error = "disconnect: broken pipe"
	summary := r.Summary()
	if !strings.Contains(summary, "9 passed, 0 failed, 0 skipped") {
		t.Errorf("summary = %q, want step tally unaffected by teardown failure", summary)
	}
	if !strings.Contains(summary, "teardown") || !strings.Contains(summary, "1 failed") {
		t.Errorf("summary = %q, want a failing teardown to be visible", summary)
	}
}

func TestJUnitExcludesTeardownFromCounts(t *testing.T) {
	r := nineStepsOneTeardown()
	path := filepath.Join(t.TempDir(), "junit.xml")
	if err := r.WriteJUnit(path); err != nil {
		t.Fatalf("WriteJUnit: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read junit: %v", err)
	}
	var doc junitSuites
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("junit is not valid XML: %v", err)
	}
	if doc.Tests != 9 {
		t.Errorf("testsuites tests = %d, want 9 (teardown must not inflate the count)", doc.Tests)
	}
	if len(doc.Suites) != 1 {
		t.Fatalf("suites = %d, want 1", len(doc.Suites))
	}
	suite := doc.Suites[0]
	if suite.Tests != 9 {
		t.Errorf("testsuite tests = %d, want 9", suite.Tests)
	}
	if len(suite.Cases) != 9 {
		t.Errorf("testcases = %d, want 9 (one per step, no teardown case)", len(suite.Cases))
	}
}

func readJUnit(t *testing.T, r *Report) junitSuites {
	t.Helper()
	path := filepath.Join(t.TempDir(), "junit.xml")
	if err := r.WriteJUnit(path); err != nil {
		t.Fatalf("WriteJUnit: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read junit: %v", err)
	}
	var doc junitSuites
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("junit is not valid XML: %v", err)
	}
	return doc
}

// A teardown failure fails the run (exit 3), so the JUnit report must not read
// green: it renders exactly one error testcase, while passing teardown steps
// stay excluded (godot-stagehand-ext-review-2026-11-327t.12).
func TestJUnitRendersTeardownFailureAsErrorCase(t *testing.T) {
	r := nineStepsOneTeardown()
	r.Teardown[0].Status = StatusFailed
	r.Teardown[0].Error = "disconnect: broken pipe"
	r.Teardown[0].ErrorKind = KindConnection
	r.Status = StatusFailed
	idx := 0
	r.Failure = &Failure{StepIndex: &idx, Phase: "teardown", Kind: KindConnection, Message: "disconnect: broken pipe"}

	doc := readJUnit(t, r)
	suite := doc.Suites[0]
	if suite.Tests != 10 || suite.Errors != 1 || suite.Failures != 0 {
		t.Errorf("counts tests/errors/failures = %d/%d/%d, want 10/1/0", suite.Tests, suite.Errors, suite.Failures)
	}
	if doc.Tests != 10 || doc.Errors != 1 {
		t.Errorf("testsuites tests/errors = %d/%d, want 10/1", doc.Tests, doc.Errors)
	}
	last := suite.Cases[len(suite.Cases)-1]
	if last.Error == nil || !strings.Contains(last.Name, "teardown") || !strings.Contains(last.ClassName, ".teardown.") {
		t.Errorf("last case = %+v, want a teardown error case", last)
	}
	if !strings.Contains(suite.SystemOut, "teardown") {
		t.Error("a failing teardown must still be visible in the junit system-out")
	}
}

// A run that dies before the first step has no steps to report; without a
// synthetic case the suite reads tests=0 failures=0 errors=0 while the job
// exits non-zero.
func TestJUnitRendersPreStepFailureAsSetupCase(t *testing.T) {
	for _, phase := range []string{"setup", "connect"} {
		t.Run(phase, func(t *testing.T) {
			r := &Report{
				Name:   "smoke",
				Status: StatusFailed,
				Steps:  []StepResult{},
				Failure: &Failure{
					Phase: phase, Kind: KindConnection, Message: "dial tcp: connection refused\nsecond line",
				},
			}
			doc := readJUnit(t, r)
			suite := doc.Suites[0]
			if suite.Tests != 1 || suite.Errors != 1 || suite.Failures != 0 {
				t.Fatalf("counts tests/errors/failures = %d/%d/%d, want 1/1/0", suite.Tests, suite.Errors, suite.Failures)
			}
			c := suite.Cases[0]
			if c.Name != "setup" || c.ClassName != "smoke.setup" {
				t.Errorf("case name/classname = %q/%q, want setup/smoke.setup", c.Name, c.ClassName)
			}
			if c.Error == nil || c.Error.Type != KindConnection || c.Error.Message != "dial tcp: connection refused" {
				t.Errorf("case error = %+v, want connection error with first-line message", c.Error)
			}
			if doc.Tests != 1 || doc.Errors != 1 {
				t.Errorf("testsuites tests/errors = %d/%d, want 1/1", doc.Tests, doc.Errors)
			}
		})
	}
}

func TestJUnitStepFailureAddsNoSetupCase(t *testing.T) {
	idx := 0
	r := &Report{
		Name:    "smoke",
		Status:  StatusFailed,
		Steps:   []StepResult{{Index: 0, Name: "go", Action: "click", Status: StatusFailed, Error: "boom", ErrorKind: KindAssertion}},
		Failure: &Failure{StepIndex: &idx, Phase: "step", Kind: KindAssertion, Message: "boom"},
	}
	if got := readJUnit(t, r).Suites[0].Tests; got != 1 {
		t.Errorf("tests = %d, want 1 (a step failure has its own case)", got)
	}
}
