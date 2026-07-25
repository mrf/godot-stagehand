package hostcompat

import (
	"slices"
	"strings"
	"testing"

	"github.com/mrf/godot-stagehand/internal/gwpop"
	"github.com/mrf/godot-stagehand/internal/scenario"
)

// The allowlist is a set of bare strings, so a typo would silently narrow the
// surface to nothing and every host scenario would fail with "not allowed" for
// an action that is in fact allowed. Pin each entry to a real action.
func TestEveryAllowedActionIsARealAction(t *testing.T) {
	local := []string{
		scenario.ActionSleep,
		scenario.ActionScreenshot,
		scenario.ActionAssertNodes,
	}
	remote := gwpop.Actions()
	for _, action := range AllowedActions() {
		if slices.Contains(local, action) || slices.Contains(remote, action) {
			continue
		}
		t.Errorf("allowed action %q is neither a gwpop action nor a runner-local one", action)
	}
}

func step(action string, with map[string]any) scenario.Step {
	return scenario.Step{Action: action, With: with}
}

func TestAllowedActionsCoverTheDeclaredSurface(t *testing.T) {
	steps := []scenario.Step{
		step("wait_for_node", map[string]any{"selector": "name:Main", "state": "exists"}),
		step("ping", nil),
		step("tree", map[string]any{"root_path": "/root", "max_depth": 3}),
		step("find", map[string]any{"selector": "class:Window"}),
		step("assert_node_count", map[string]any{
			"selector": "class:Window", "operator": "greater_than", "expected": 0,
		}),
		step("click", map[string]any{"selector": "class:Button"}),
		step("press_key", map[string]any{"key": "escape"}),
		step("press_action", map[string]any{"action": "ui_accept"}),
		step("type_text", map[string]any{"text": "stagehand"}),
		step("mouse_move", map[string]any{"x": 10, "y": 10}),
		step("screenshot", map[string]any{"output": "after-input.png"}),
		step("sleep", map[string]any{"duration_ms": 200}),
		step("get_performance", map[string]any{"monitors": []any{"TIME_FPS"}}),
	}
	if err := ValidateSteps(steps); err != nil {
		t.Fatalf("in-surface steps rejected: %v", err)
	}
}

// Everything below asserts something about the HOST APPLICATION rather than
// about Stagehand. Their bugs are not ours, and a suite that asserts on them
// goes red for reasons nobody in this repo can fix — which is how a suite like
// this gets muted and then deleted.
func TestValidateStepsRejectsHostBehaviourAssertions(t *testing.T) {
	tests := []struct {
		name   string
		step   scenario.Step
		errHas string
	}{
		{
			"assert_property reads host state",
			step("assert_property", map[string]any{
				"selector": "name:Title", "property": "text",
				"operator": "equals", "expected": "Pixelorama",
			}),
			"assert_property",
		},
		{
			"set_property mutates host state",
			step("set_property", map[string]any{
				"selector": "name:Title", "property": "text", "value": "x",
			}),
			"set_property",
		},
		{
			"evaluate runs host code",
			step("evaluate", map[string]any{"expression": "1+1"}),
			"evaluate",
		},
		{
			"call_method drives host logic",
			step("call_method", map[string]any{"selector": "name:Main", "method": "quit"}),
			"call_method",
		},
		{
			"screenshot_diff pins the host's pixels",
			step("screenshot_diff", map[string]any{"name": "main"}),
			"screenshot_diff",
		},
		{
			"save_baseline pins the host's pixels",
			step("save_baseline", map[string]any{"name": "main"}),
			"save_baseline",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSteps([]scenario.Step{tc.step})
			if err == nil || !strings.Contains(err.Error(), tc.errHas) {
				t.Fatalf("expected a rejection mentioning %q, got %v", tc.errHas, err)
			}
		})
	}
}

// assert_node_count is in-surface only in its structural form: "Stagehand can
// see at least one node of this kind". Pinning an exact count pins the host's
// UI layout, which is host behaviour by another name.
func TestAssertNodeCountMustStayStructural(t *testing.T) {
	tests := []struct {
		name    string
		with    map[string]any
		wantErr bool
	}{
		{"greater_than", map[string]any{"selector": "class:Window", "operator": "greater_than", "expected": 0}, false},
		{"gte", map[string]any{"selector": "class:Window", "operator": "gte", "expected": 1}, false},
		{"equals pins a layout", map[string]any{"selector": "class:Window", "operator": "equals", "expected": 3}, true},
		{"less_than pins a layout", map[string]any{"selector": "class:Window", "operator": "less_than", "expected": 9}, true},
		{"not_equals pins a layout", map[string]any{"selector": "class:Window", "operator": "not_equals", "expected": 0}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSteps([]scenario.Step{step("assert_node_count", tc.with)})
			if tc.wantErr && err == nil {
				t.Fatal("expected a rejection")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected rejection: %v", err)
			}
		})
	}
}

func TestValidateStepsReportsTheOffendingIndex(t *testing.T) {
	steps := []scenario.Step{
		step("tree", map[string]any{"root_path": "/root"}),
		step("evaluate", map[string]any{"expression": "1"}),
	}
	err := ValidateSteps(steps)
	if err == nil || !strings.Contains(err.Error(), "step 2") {
		t.Fatalf("expected the 1-based step index in %v", err)
	}
}

func TestValidateScenarioChecksTeardownToo(t *testing.T) {
	sc := &scenario.Scenario{
		Steps:    []scenario.Step{step("tree", map[string]any{"root_path": "/root"})},
		Teardown: []scenario.Step{step("set_property", map[string]any{"selector": "x", "property": "y", "value": 1})},
	}
	err := ValidateScenario(sc)
	if err == nil || !strings.Contains(err.Error(), "teardown") {
		t.Fatalf("expected a teardown rejection, got %v", err)
	}
}

// A host-compat scenario must launch its own instance: connect mode would
// attach to whatever game happens to hold the port, which in CI is a race and
// in a developer's shell is someone else's game.
func TestValidateScenarioRequiresLaunchMode(t *testing.T) {
	sc := &scenario.Scenario{
		Target: scenario.Target{Mode: scenario.ModeConnect},
		Steps:  []scenario.Step{step("tree", map[string]any{"root_path": "/root"})},
	}
	err := ValidateScenario(sc)
	if err == nil || !strings.Contains(err.Error(), "launch") {
		t.Fatalf("expected a launch-mode requirement, got %v", err)
	}
}

// allow_unsafe opens evaluate and arbitrary call_method on a third-party
// codebase we did not write. The surface has no use for either.
func TestValidateScenarioRejectsAllowUnsafe(t *testing.T) {
	sc := &scenario.Scenario{
		Target: scenario.Target{Mode: scenario.ModeLaunch, AllowUnsafe: true},
		Steps:  []scenario.Step{step("tree", map[string]any{"root_path": "/root"})},
	}
	err := ValidateScenario(sc)
	if err == nil || !strings.Contains(err.Error(), "allow_unsafe") {
		t.Fatalf("expected an allow_unsafe rejection, got %v", err)
	}
}
