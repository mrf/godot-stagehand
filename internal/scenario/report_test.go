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
	r := &Report{
		Name:   "smoke",
		Status: StatusFailed,
		Failure: &Failure{
			StepIndex: 1,
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
	r := &Report{
		Name:   "smoke",
		Status: StatusFailed,
		Failure: &Failure{
			StepIndex: 0,
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

func TestJUnitSurfacesTeardownFailureInSystemOut(t *testing.T) {
	r := nineStepsOneTeardown()
	r.Teardown[0].Status = StatusFailed
	r.Teardown[0].Error = "disconnect: broken pipe"
	r.Status = StatusFailed
	r.Failure = &Failure{StepIndex: 0, Phase: "teardown", Kind: KindConnection, Message: "disconnect: broken pipe"}

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
	if doc.Suites[0].Tests != 9 || doc.Suites[0].Failures != 0 {
		t.Errorf("counts tests/failures = %d/%d, want 9/0 (teardown failure must not inflate the excluded count)",
			doc.Suites[0].Tests, doc.Suites[0].Failures)
	}
	if !strings.Contains(doc.Suites[0].SystemOut, "teardown") {
		t.Error("a failing teardown must still be visible in the junit system-out")
	}
}
