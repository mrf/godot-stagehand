package mcpserver

import (
	"strings"
	"testing"
)

// TestLaunchToolIsolatesUserDataByDefault pins the tool-level contract: agents
// get a private user:// unless they explicitly ask to share the project's real
// one, so two agents launching the same project cannot corrupt each other's
// saves and settings.
func TestLaunchToolIsolatesUserDataByDefault(t *testing.T) {
	property, ok := launchTool.InputSchema.Properties["share_user_data"]
	if !ok {
		t.Fatal("godot_launch schema must expose share_user_data")
	}
	propertySchema, ok := property.(map[string]any)
	if !ok {
		t.Fatalf("godot_launch share_user_data schema has type %T, want map[string]any", property)
	}
	if got := propertySchema["default"]; got != false {
		t.Fatalf("godot_launch share_user_data default = %v, want false (isolated)", got)
	}
	description, ok := propertySchema["description"].(string)
	if !ok {
		t.Fatalf("godot_launch share_user_data description has type %T, want string", propertySchema["description"])
	}
	if !strings.Contains(description, "user://") {
		t.Errorf("share_user_data description should explain the user:// trade-off, got %q", description)
	}
}
