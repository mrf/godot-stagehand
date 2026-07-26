package scenario

import (
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
