package hostcompat

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mrf/godot-stagehand/internal/scenario"
)

// The host-compat assertion surface. A host scenario may assert that Stagehand
// can install, launch, connect, enumerate the tree, drive input and capture a
// frame — and nothing else. It must never assert that the host application
// behaves correctly: their bugs are not ours, and a suite that goes red for
// reasons nobody here can fix is a suite people learn to ignore.
//
// The line is "does the mechanism work", not "does the app do the right thing".
// That has a real cost, stated plainly: the suite proves an input event was
// delivered, not that the host reacted to it. Effect-level regressions belong
// in fixtures we own under testdata/test_project, where asserting on behaviour
// is legitimate because the behaviour is ours.
var allowedActions = []string{
	// connect
	"ping",
	// enumerate
	"tree",
	"find",
	"wait_for_node",
	scenario.ActionAssertNodes,
	// drive input
	"click",
	"press_key",
	"press_action",
	"type_text",
	"mouse_move",
	"touch",
	// capture
	scenario.ActionScreenshot,
	// pacing and engine-level counters (not host state)
	scenario.ActionSleep,
	"get_performance",
}

// structuralCountOperators are the only assert_node_count operators in surface.
// "at least one node of this kind is visible to Stagehand" is a claim about our
// enumeration; an exact count is a claim about the host's UI layout.
var structuralCountOperators = []string{"greater_than", "gte"}

// AllowedActions returns the host-compat action allowlist.
func AllowedActions() []string { return slices.Clone(allowedActions) }

// ValidateScenario checks a host-compat scenario against the narrow surface,
// including its target configuration and teardown.
func ValidateScenario(sc *scenario.Scenario) error {
	// Launch mode gives the run its own instance on an auto-assigned port.
	// Connect mode would attach to whatever game happens to hold the port,
	// which in CI is a race and on a developer's machine is someone else's game.
	if sc.Target.Mode != "" && sc.Target.Mode != scenario.ModeLaunch {
		return fmt.Errorf("host-compat scenarios must use launch mode, got %q", sc.Target.Mode)
	}
	// allow_unsafe opens evaluate and arbitrary call_method against a codebase
	// we did not write. Nothing in the surface needs either.
	if sc.Target.AllowUnsafe {
		return fmt.Errorf("host-compat scenarios must not set allow_unsafe")
	}
	if err := ValidateSteps(sc.Steps); err != nil {
		return err
	}
	if err := ValidateSteps(sc.Teardown); err != nil {
		return fmt.Errorf("teardown: %w", err)
	}
	return nil
}

// ValidateSteps checks a step list against the surface. Errors name the 1-based
// step index so a rejection points at a line a reader can find.
func ValidateSteps(steps []scenario.Step) error {
	for i, step := range steps {
		if err := validateStep(step); err != nil {
			return fmt.Errorf("step %d (%s): %w", i+1, step.Label(), err)
		}
	}
	return nil
}

func validateStep(step scenario.Step) error {
	if !slices.Contains(allowedActions, step.Action) {
		return fmt.Errorf("action %q is outside the host-compat surface (allowed: %s)",
			step.Action, strings.Join(allowedActions, ", "))
	}
	if step.Action == scenario.ActionAssertNodes {
		operator, _ := step.With["operator"].(string)
		if !slices.Contains(structuralCountOperators, operator) {
			return fmt.Errorf(
				"assert_node_count operator %q pins the host's layout; use %s",
				operator, strings.Join(structuralCountOperators, " or "))
		}
	}
	return nil
}
