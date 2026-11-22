package mcpserver

import (
	"slices"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/mrf/godot-stagehand/internal/gwpop"
	"github.com/mrf/godot-stagehand/internal/scenario"
)

// scenarioTwins maps every scenario action that has an MCP tool onto that
// tool. docs/cli.md promises a step's parameters match the tool's arguments;
// this is the guard that keeps the promise from drifting again.
var scenarioTwins = map[string]mcp.Tool{
	"game_state":         getGameStateTool,
	"tree":               getTreeTool,
	"find":               findNodesTool,
	"get_property":       getPropertyTool,
	"set_property":       setPropertyTool,
	"call_method":        callMethodTool,
	"evaluate":           evaluateTool,
	"change_scene":       changeSceneTool,
	"click":              clickTool,
	"press_key":          pressKeyTool,
	"focus_window":       focusWindowTool,
	"press_action":       pressActionTool,
	"type_text":          typeTextTool,
	"mouse_move":         mouseMoveTool,
	"touch":              touchTool,
	"wait_for_node":      waitForNodeTool,
	"wait_for_signal":    waitForSignalTool,
	"wait_for_property":  waitForPropertyTool,
	"get_performance":    getPerformanceTool,
	"assert_performance": assertPerformanceTool,
	"record_start":       recordStartTool,
	"record_stop":        recordStopTool,
	"replay":             replayTool,
	"screenshot":         screenshotTool,
	"save_baseline":      saveBaselineTool,
	"screenshot_diff":    screenshotDiffTool,
}

// scenarioParamExempt lists MCP arguments a scenario step deliberately lacks.
var scenarioParamExempt = map[string][]string{
	// Targeting an instance is the scenario's own session concern.
	"*": {"instance_id"},
	// doScreenshot always captures the full viewport unless a selector crops it.
	"screenshot": {"full_page"},
}

func TestScenarioActionsAcceptMCPArgumentNames(t *testing.T) {
	for action, tool := range scenarioTwins {
		spec, ok := scenario.SpecFor(action)
		if !ok {
			t.Errorf("%s: no scenario action, but MCP tool %s exists", action, tool.Name)
			continue
		}
		exempt := append(slices.Clone(scenarioParamExempt["*"]), scenarioParamExempt[action]...)
		for name := range tool.InputSchema.Properties {
			if !slices.Contains(exempt, name) && !spec.AcceptsParam(name) {
				t.Errorf("%s: scenario step rejects %s's argument %q", action, tool.Name, name)
			}
		}
	}
}

// A newly added gwpop action cannot skip the parity check.
func TestScenarioPassThroughActionsHaveParityCoverage(t *testing.T) {
	for _, action := range gwpop.Actions() {
		if _, covered := scenarioTwins[action]; covered || action == "ping" {
			continue // ping is the handshake; its MCP twins take connection config
		}
		t.Errorf("gwpop action %q is not in scenarioTwins; add its MCP twin or exempt it", action)
	}
}

func TestScenarioDeprecatedParamNamesStillAccepted(t *testing.T) {
	for action, names := range map[string][]string{
		"tree":         {"properties"},
		"replay":       {"input_path"},
		"record_start": {"output_path"},
	} {
		spec, _ := scenario.SpecFor(action)
		for _, name := range names {
			if !spec.AcceptsParam(name) {
				t.Errorf("%s: deprecated parameter %q no longer accepted", action, name)
			}
		}
	}
}

func TestReplayStepParamsNormaliseToWire(t *testing.T) {
	spec, _ := gwpop.Lookup("replay")
	for _, key := range []string{"recording_path", "input_path"} {
		got, err := spec.Params(map[string]any{key: "run1.json", "speed": 2.0, "wait_for_ready": false})
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if got["input_path"] != "user://stagehand_recordings/run1.json" || got["speed"] != 2.0 || got["wait_for_ready"] != false {
			t.Errorf("%s: wire params = %v", key, got)
		}
		if _, leaked := got["recording_path"]; leaked {
			t.Errorf("%s: alias leaked onto the wire: %v", key, got)
		}
	}
	if _, err := spec.Params(map[string]any{"recording_path": "a.json", "input_path": "b.json"}); err == nil {
		t.Error("both recording_path and input_path supplied should be a usage error")
	}
	if _, err := spec.Params(map[string]any{}); err == nil {
		t.Error("replay without a recording should be a usage error")
	}
	for _, speed := range []float64{0, 1000} {
		if _, err := spec.Params(map[string]any{"recording_path": "a.json", "speed": speed}); err == nil {
			t.Errorf("speed %v is outside the MCP bounds and should be rejected", speed)
		}
	}
}

func TestTreeStepIncludePropertiesNormalisesToWire(t *testing.T) {
	spec, _ := gwpop.Lookup("tree")
	got, err := spec.Params(map[string]any{"include_properties": []any{"text"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["properties"]; !ok {
		t.Errorf("include_properties not mapped to wire `properties`: %v", got)
	}
	if _, ok := got["include_properties"]; ok {
		t.Errorf("alias leaked onto the wire: %v", got)
	}
	if _, err := spec.Params(map[string]any{"include_properties": []any{"a"}, "properties": []any{"b"}}); err == nil {
		t.Error("both spellings supplied should be a usage error")
	}
}

func TestRecordStartOutputPathOptionalAndTakesMouseMove(t *testing.T) {
	spec, _ := gwpop.Lookup("record_start")
	got, err := spec.Params(map[string]any{"include_mouse_move": true})
	if err != nil {
		t.Fatalf("record_start with no output_path: %v", err)
	}
	if got["include_mouse_move"] != true {
		t.Errorf("wire params = %v", got)
	}
}
