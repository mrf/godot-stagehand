package mcpserver

import (
	"testing"
)

// TestSetPropertyValueDeclaresEveryJSONType pins the Go-side half of
// godot-stagehand-set-property-value-stringified-e7er.
//
// mcp.WithAny emits a property schema with no "type" key at all. A client that
// has no declared type for an argument has nothing to marshal against, and at
// least one — the reporter's — sends the value as raw JSON *text* instead of a
// native JSON value, so every non-String, non-bool target hard-fails the
// addon's conversion gate. The sibling argument that marshals correctly,
// godot_call_method's "args", differs in exactly this respect: it is declared
// with mcp.WithArray and therefore carries "type": "array".
//
// Declaring the full type union keeps "value" polymorphic while giving clients
// something concrete to serialize against.
func TestSetPropertyValueDeclaresEveryJSONType(t *testing.T) {
	// wait_for_property's expected_value shares the defect and the fix: a
	// stringified "5" never compares equal to an int 5, so that wait would sit
	// out its full timeout instead of matching.
	for _, tt := range []struct {
		tool     string
		schema   map[string]any
		property string
	}{
		{tool: "godot_set_property", schema: setPropertyTool.InputSchema.Properties, property: "value"},
		{tool: "godot_wait_for_property", schema: waitForPropertyTool.InputSchema.Properties, property: "expected_value"},
	} {
		t.Run(tt.tool, func(t *testing.T) {
			property, ok := tt.schema[tt.property]
			if !ok {
				t.Fatalf("%s schema must expose %s", tt.tool, tt.property)
			}
			schema, ok := property.(map[string]any)
			if !ok {
				t.Fatalf("%s %s schema has type %T, want map[string]any", tt.tool, tt.property, property)
			}

			declared, ok := schema["type"].([]string)
			if !ok {
				t.Fatalf("%s %s schema must declare a JSON type union, got type=%#v", tt.tool, tt.property, schema["type"])
			}

			got := make(map[string]bool, len(declared))
			for _, name := range declared {
				got[name] = true
			}
			for _, want := range []string{"boolean", "integer", "number", "string", "object", "array", "null"} {
				if !got[want] {
					t.Errorf("%s %s schema omits JSON type %q (declared: %v)", tt.tool, tt.property, want, declared)
				}
			}
		})
	}
}

// TestCallMethodArgsRemainsTypedArray guards the contrast the bug report used
// as its control: call_method's args marshal natively because they are typed.
func TestCallMethodArgsRemainsTypedArray(t *testing.T) {
	property, ok := callMethodTool.InputSchema.Properties["args"]
	if !ok {
		t.Fatal("godot_call_method schema must expose args")
	}
	schema, ok := property.(map[string]any)
	if !ok {
		t.Fatalf("godot_call_method args schema has type %T, want map[string]any", property)
	}
	if got := schema["type"]; got != "array" {
		t.Fatalf("godot_call_method args type = %#v, want \"array\"", got)
	}
}
