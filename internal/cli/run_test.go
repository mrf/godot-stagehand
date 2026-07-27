package cli

import (
	"strings"
	"testing"

	"github.com/mrf/godot-stagehand/internal/scenario"
)

// TestScenarioFailureErrorDoesNotDoubleTheWordStep reproduces
// godot-stagehand-1lwq: run.go:91 formatted "%s step %d" against a Phase that
// is literally "step" for the main phase, printing "failed at step step 1".
func TestScenarioFailureErrorDoesNotDoubleTheWordStep(t *testing.T) {
	idx := 1
	e := &scenarioFailure{
		report: &scenario.Report{
			Name: "smoke",
			Failure: &scenario.Failure{
				StepIndex: &idx,
				Phase:     "step",
				Kind:      scenario.KindAssertion,
				Message:   "boom",
			},
		},
	}
	if want := "failed at step 1 (assertion): boom"; !strings.Contains(e.Error(), want) {
		t.Fatalf("Error() = %q, want it to contain %q", e.Error(), want)
	}
}

func TestScenarioFailureErrorLabelsTeardownStepCorrectly(t *testing.T) {
	idx := 0
	e := &scenarioFailure{
		report: &scenario.Report{
			Name: "smoke",
			Failure: &scenario.Failure{
				StepIndex: &idx,
				Phase:     "teardown",
				Kind:      scenario.KindAssertion,
				Message:   "boom",
			},
		},
	}
	if want := "failed at teardown step 0 (assertion): boom"; !strings.Contains(e.Error(), want) {
		t.Fatalf("Error() = %q, want it to contain %q", e.Error(), want)
	}
}

// TestScenarioFailureErrorOmitsFakeStepIndexForPreStepFailure reproduces
// godot-stagehand-07v1: a connect failure happens before any step runs, so
// the CLI's human-readable line must not surface the -1 step sentinel.
func TestScenarioFailureErrorOmitsFakeStepIndexForPreStepFailure(t *testing.T) {
	e := &scenarioFailure{
		report: &scenario.Report{
			Name: "unreachable",
			Failure: &scenario.Failure{
				Phase:   "connect",
				Kind:    scenario.KindConnection,
				Message: "dial tcp 127.0.0.1:1: connect: connection refused",
			},
		},
	}
	if want := "failed at connect phase (connection):"; !strings.Contains(e.Error(), want) {
		t.Fatalf("Error() = %q, want it to contain %q", e.Error(), want)
	}
	if strings.Contains(e.Error(), "-1") {
		t.Errorf("Error() = %q, must not leak the -1 step sentinel", e.Error())
	}
}
