package mcpserver

import (
	"strings"
	"testing"
)

// TestLaunchToolExposesScene pins the boot-scene knob: extra_args lands after
// "--" where Godot treats it as a user arg, so a scene needs its own parameter.
func TestLaunchToolExposesScene(t *testing.T) {
	property, ok := launchTool.InputSchema.Properties["scene"]
	if !ok {
		t.Fatal("godot_launch schema must expose scene")
	}
	schema, ok := property.(map[string]any)
	if !ok {
		t.Fatalf("scene schema has type %T, want map[string]any", property)
	}
	if got := schema["type"]; got != "string" {
		t.Fatalf("scene type = %v, want string", got)
	}
}

// TestLaunchToolDocumentsExtraArgsSide keeps the schema honest about which side
// of "--" each parameter lands on.
func TestLaunchToolDocumentsExtraArgsSide(t *testing.T) {
	extra, _ := launchTool.InputSchema.Properties["extra_args"].(map[string]any)
	desc, _ := extra["description"].(string)
	if !strings.Contains(desc, `after "--"`) {
		t.Errorf("extra_args description %q must say it lands after \"--\"", desc)
	}
	scene, _ := launchTool.InputSchema.Properties["scene"].(map[string]any)
	sdesc, _ := scene["description"].(string)
	if !strings.Contains(sdesc, `before "--"`) {
		t.Errorf("scene description %q must say it lands before \"--\"", sdesc)
	}
}
