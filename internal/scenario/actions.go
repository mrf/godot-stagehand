package scenario

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mrf/godot-stagehand/internal/gwpop"
	"github.com/mrf/godot-stagehand/internal/visual"
)

// Runner-local actions: everything that needs work on the Go side (a file
// write, a comparison, a sleep) rather than a single pass-through RPC.
const (
	ActionSleep          = "sleep"
	ActionScreenshot     = "screenshot"
	ActionSaveBaseline   = "save_baseline"
	ActionScreenshotDiff = "screenshot_diff"
	ActionAssertProperty = "assert_property"
	ActionAssertNodes    = "assert_node_count"
)

// localSpecs reuses gwpop.Spec purely for its parameter contract; Method is
// empty because these actions are not a single pass-through RPC.
var localSpecs = map[string]gwpop.Spec{
	ActionSleep: {
		Action:   ActionSleep,
		Required: []string{"duration_ms"},
		Summary:  "Pause the run for a fixed duration",
	},
	// This is the spec SpecFor actually resolves for "screenshot" — it wins
	// over gwpop's same-named entry, which exists only for gwpop.Capture's
	// internal use (see the comment on that entry in gwpop.go). Scenarios
	// have no full_page knob: doScreenshot always captures full-viewport
	// unless selector crops it.
	ActionScreenshot: {
		Action:    ActionScreenshot,
		Optional:  []string{"selector", "output"},
		Selectors: []string{"selector"},
		Summary:   "Capture the viewport and write it to the artifact directory",
	},
	ActionSaveBaseline: {
		Action:    ActionSaveBaseline,
		Required:  []string{"name"},
		Optional:  []string{"selector"},
		Selectors: []string{"selector"},
		Summary:   "Capture the viewport and store it as a named baseline",
	},
	ActionScreenshotDiff: {
		Action:    ActionScreenshotDiff,
		Required:  []string{"name"},
		Optional:  []string{"selector", "threshold", "pixel_sensitivity"},
		Selectors: []string{"selector"},
		Summary:   "Compare the viewport against a named baseline",
	},
	ActionAssertProperty: {
		Action:    ActionAssertProperty,
		Required:  []string{"selector", "property", "operator"},
		Optional:  []string{"expected"},
		Selectors: []string{"selector"},
		Summary:   "Read a property once and assert a condition on it",
	},
	ActionAssertNodes: {
		Action:    ActionAssertNodes,
		Required:  []string{"selector", "operator"},
		Optional:  []string{"expected"},
		Selectors: []string{"selector"},
		Summary:   "Assert how many nodes a selector matches",
	},
}

// capturesScreenshot reports whether an action needs a rendered frame.
func capturesScreenshot(action string) bool {
	switch action {
	case ActionScreenshot, ActionSaveBaseline, ActionScreenshotDiff:
		return true
	default:
		return false
	}
}

// validateStepSemantics catches authoring mistakes the generic parameter
// contract cannot see: an unknown operator, an expected value that an operator
// requires, or an output path that would escape the artifact directory.
func validateStepSemantics(step Step) error {
	switch step.Action {
	case ActionSleep:
		if _, ok := asPositiveInt(step.With["duration_ms"]); !ok {
			return fmt.Errorf("duration_ms must be a positive number of milliseconds")
		}
	case ActionScreenshot:
		if output, ok := step.With["output"]; ok {
			path, ok := output.(string)
			if !ok {
				return fmt.Errorf("output must be a string")
			}
			if err := validateArtifactPath(path); err != nil {
				return err
			}
		}
	case ActionSaveBaseline, ActionScreenshotDiff:
		name, ok := step.With["name"].(string)
		if !ok {
			return fmt.Errorf("name must be a string")
		}
		// Baseline names are filename stems, not paths: validate them against
		// the stricter allowlist internal/visual enforces at write time so the
		// scenario fails at parse time rather than mid-run.
		if err := visual.ValidateName(name); err != nil {
			return err
		}
	case ActionAssertProperty, ActionAssertNodes:
		return validateAssertion(step)
	case "wait_for_property":
		operator, _ := step.With["operator"].(string)
		return validateOperator(operator, step.With, "expected_value")
	}
	return nil
}

func validateAssertion(step Step) error {
	operator, ok := step.With["operator"].(string)
	if !ok {
		return fmt.Errorf("operator must be a string")
	}
	if step.Action == ActionAssertNodes && !slices.Contains([]string{"equals", "not_equals", "greater_than", "less_than"}, operator) {
		return fmt.Errorf("operator %q is not a numeric comparison; assert_node_count accepts equals, not_equals, greater_than, less_than", operator)
	}
	return validateOperator(operator, step.With, "expected")
}

func validateOperator(operator string, with map[string]any, expectedKey string) error {
	if !slices.Contains(gwpop.Operators, operator) {
		return fmt.Errorf("unknown operator %q (known: %s)", operator, strings.Join(gwpop.Operators, ", "))
	}
	if operator == "exists" {
		return nil
	}
	if _, ok := with[expectedKey]; !ok {
		return fmt.Errorf("operator %q requires %q", operator, expectedKey)
	}
	return nil
}

// validateArtifactPath rejects names that would write outside the run's
// artifact directory. Scenario files are data and may come from a pull
// request; a step must not be able to overwrite an arbitrary file.
func validateArtifactPath(path string) error {
	if path == "" {
		return fmt.Errorf("path must not be empty")
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`) || strings.Contains(path, ":") {
		return fmt.Errorf("path %q must be relative to the artifact directory", path)
	}
	for _, segment := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return fmt.Errorf("path %q must not escape the artifact directory", path)
		}
	}
	return nil
}

func asPositiveInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n <= 0 {
			return 0, false
		}
		return int(n), true
	case int:
		if n <= 0 {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

func asFloatOr(v any, fallback float64) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return fallback
	}
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
