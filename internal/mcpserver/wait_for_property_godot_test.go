//go:build godot

package mcpserver

import (
	"encoding/json"
	"testing"
)

// TestMCPWaitForPropertyAcceptsStringifiedExpectedValue is the end-to-end
// regression test for godot-stagehand-wait-for-property-stringified-expected-60sz.
//
// godot_wait_for_property's expected_value schema got the same Go-side fix as
// set_property's value (see TestMCPSetPropertyAcceptsStringifiedClientValues),
// but a client that predates that fix — or one that still marshals loosely —
// can send expected_value as a JSON string even for a numeric or boolean
// property. waiter.gd compares actual_value == expected_value directly (and
// gt/lt guard on "expected_value is float or int"), so a stringified "5" never
// equals an int 5 and the wait silently sits out its full timeout instead of
// succeeding immediately. Each case uses a short timeout so an unfixed waiter
// fails fast rather than hanging the test suite.
func TestMCPWaitForPropertyAcceptsStringifiedExpectedValue(t *testing.T) {
	srv := startMCPServerWithGodot(t)

	tests := []struct {
		name          string
		property      string
		operator      string
		expectedValue string
	}{
		{name: "equals_int", property: "count_prop", operator: "equals", expectedValue: "5"},
		{name: "equals_float", property: "ratio_prop", operator: "equals", expectedValue: "0.5"},
		{name: "equals_bool_true", property: "flag_prop", operator: "equals", expectedValue: "true"},
		{name: "greater_than_stringified", property: "count_prop", operator: "greater_than", expectedValue: "3"},
		{name: "less_than_stringified", property: "count_prop", operator: "less_than", expectedValue: "10"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resultText := callToolThroughMCP(t, srv, "godot_wait_for_property", map[string]any{
				"selector":         "/root/TestScene/PropertyTarget",
				"property":         tt.property,
				"operator":         tt.operator,
				"expected_value":   tt.expectedValue,
				"timeout_ms":       500,
				"poll_interval_ms": 50,
			})

			var result struct {
				Success      bool   `json:"success"`
				MetCondition bool   `json:"met_condition"`
				ErrorCode    string `json:"error_code"`
			}
			if err := json.Unmarshal([]byte(resultText), &result); err != nil {
				t.Fatalf("decode wait_for_property result %q: %v", resultText, err)
			}
			if !result.Success || !result.MetCondition {
				t.Fatalf(
					"wait_for_property(%s %s %q) did not match a stringified expected_value: %s",
					tt.property, tt.operator, tt.expectedValue, resultText,
				)
			}
		})
	}
}

// TestMCPWaitForPropertyEqualsStringPropertyStaysVerbatim guards the
// target-awareness constraint inherited from e7er: "equals" against a String
// property must still compare the stringified expected_value literally, not
// JSON-decode it first. text_prop holds "50" as the two-character string, and
// only a native string "50" — not the number 50 — should match it.
func TestMCPWaitForPropertyEqualsStringPropertyStaysVerbatim(t *testing.T) {
	srv := startMCPServerWithGodot(t)

	setText := callToolThroughMCP(t, srv, "godot_set_property", map[string]any{
		"selector": "/root/TestScene/PropertyTarget",
		"property": "text_prop",
		"value":    "50",
	})
	var setResult struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal([]byte(setText), &setResult); err != nil {
		t.Fatalf("decode set_property result %q: %v", setText, err)
	}
	if !setResult.Success {
		t.Fatalf("set_property returned success=false: %s", setText)
	}

	resultText := callToolThroughMCP(t, srv, "godot_wait_for_property", map[string]any{
		"selector":         "/root/TestScene/PropertyTarget",
		"property":         "text_prop",
		"operator":         "equals",
		"expected_value":   "50",
		"timeout_ms":       500,
		"poll_interval_ms": 50,
	})
	var result struct {
		Success      bool `json:"success"`
		MetCondition bool `json:"met_condition"`
	}
	if err := json.Unmarshal([]byte(resultText), &result); err != nil {
		t.Fatalf("decode wait_for_property result %q: %v", resultText, err)
	}
	if !result.Success || !result.MetCondition {
		t.Fatalf("wait_for_property(text_prop equals \"50\") should match the literal string: %s", resultText)
	}
}
