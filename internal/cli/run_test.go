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
	e := &scenarioFailure{
		report: &scenario.Report{
			Name: "smoke",
			Failure: &scenario.Failure{
				StepIndex: 1,
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
	e := &scenarioFailure{
		report: &scenario.Report{
			Name: "smoke",
			Failure: &scenario.Failure{
				StepIndex: 0,
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
