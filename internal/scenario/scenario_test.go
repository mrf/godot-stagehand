package scenario

import (
	"strings"
	"testing"
)

func TestParseRejectsUnknownField(t *testing.T) {
	_, err := Parse([]byte(`{
		"target": {"mode": "launch", "project_path": "p"},
		"stepz": []
	}`))
	if err == nil {
		t.Fatal("Parse accepted a misspelled top-level field")
	}
}

func TestParseRejectsUnknownAction(t *testing.T) {
	_, err := Parse([]byte(`{
		"target": {"mode": "launch", "project_path": "p"},
		"steps": [{"action": "teleport"}]
	}`))
	if err == nil {
		t.Fatal("Parse accepted an unknown action")
	}
	if !strings.Contains(err.Error(), "teleport") {
		t.Errorf("error %q does not name the unknown action", err)
	}
}

func TestParseRejectsUnknownStepParameter(t *testing.T) {
	_, err := Parse([]byte(`{
		"target": {"mode": "launch", "project_path": "p"},
		"steps": [{"action": "find", "with": {"selector": "class:Button", "limitt": 3}}]
	}`))
	if err == nil {
		t.Fatal("Parse accepted a misspelled step parameter")
	}
}

func TestParseRejectsInvalidSelectorBeforeLaunch(t *testing.T) {
	_, err := Parse([]byte(`{
		"target": {"mode": "launch", "project_path": "p"},
		"steps": [{"action": "click", "with": {"selector": "class:"}}]
	}`))
	if err == nil {
		t.Fatal("Parse accepted an invalid selector")
	}
}

func TestParseRejectsHeadlessScreenshotScenario(t *testing.T) {
	_, err := Parse([]byte(`{
		"target": {"mode": "launch", "project_path": "p"},
		"steps": [{"action": "save_baseline", "with": {"name": "menu"}}]
	}`))
	if err == nil {
		t.Fatal("Parse accepted a headless scenario that captures frames")
	}
	if !strings.Contains(err.Error(), "headless") {
		t.Errorf("error %q does not explain the headless conflict", err)
	}
}

func TestParseAllowsScreenshotsWithVisibleWindow(t *testing.T) {
	if _, err := Parse([]byte(`{
		"target": {"mode": "launch", "project_path": "p", "headless": false},
		"steps": [{"action": "save_baseline", "with": {"name": "menu"}}]
	}`)); err != nil {
		t.Fatalf("Parse rejected a valid visual scenario: %v", err)
	}
}

func TestParseRequiresExplicitPortInConnectMode(t *testing.T) {
	_, err := Parse([]byte(`{
		"target": {"mode": "connect"},
		"steps": [{"action": "tree"}]
	}`))
	if err == nil {
		t.Fatal("connect mode accepted a scenario with no explicit port")
	}
	if !strings.Contains(err.Error(), "26700") {
		t.Errorf("error %q does not explain why the shared default is refused", err)
	}
}

func TestParseRejectsLaunchSettingsInConnectMode(t *testing.T) {
	_, err := Parse([]byte(`{
		"target": {"mode": "connect", "port": 26701, "project_path": "p"},
		"steps": [{"action": "tree"}]
	}`))
	if err == nil {
		t.Fatal("connect mode accepted launch-only settings")
	}
}

func TestParseRejectsAssertionWithoutExpected(t *testing.T) {
	_, err := Parse([]byte(`{
		"target": {"mode": "launch", "project_path": "p"},
		"steps": [{"action": "assert_property", "with": {"selector": "name:X", "property": "text", "operator": "equals"}}]
	}`))
	if err == nil {
		t.Fatal("assert_property accepted equals with no expected value")
	}
}

func TestParseRejectsUnknownOperator(t *testing.T) {
	_, err := Parse([]byte(`{
		"target": {"mode": "launch", "project_path": "p"},
		"steps": [{"action": "assert_property", "with": {"selector": "name:X", "property": "text", "operator": "approximately", "expected": 1}}]
	}`))
	if err == nil {
		t.Fatal("assert_property accepted an unknown operator")
	}
}

func TestParseRejectsEscapingArtifactPath(t *testing.T) {
	for _, output := range []string{"../escape.png", "/tmp/escape.png"} {
		_, err := Parse([]byte(`{
			"target": {"mode": "launch", "project_path": "p", "headless": false},
			"steps": [{"action": "screenshot", "with": {"output": "` + output + `"}}]
		}`))
		if err == nil {
			t.Errorf("screenshot accepted an escaping output path %q", output)
		}
	}
}

// TestParseRejectsUnsafeBaselineName pins scenario validation to the same
// allowlist internal/visual enforces, so a bad name fails at parse time rather
// than mid-run.
func TestParseRejectsUnsafeBaselineName(t *testing.T) {
	for _, action := range []string{"save_baseline", "screenshot_diff"} {
		for _, name := range []string{"../escape", `..\\escape`, "sub/menu", ".hidden", "main menu", ""} {
			_, err := Parse([]byte(`{
				"target": {"mode": "launch", "project_path": "p", "headless": false},
				"steps": [{"action": "` + action + `", "with": {"name": "` + name + `"}}]
			}`))
			if err == nil {
				t.Errorf("%s accepted unsafe baseline name %q", action, name)
			}
		}
	}
}

func TestParseAcceptsSafeBaselineName(t *testing.T) {
	if _, err := Parse([]byte(`{
		"target": {"mode": "launch", "project_path": "p", "headless": false},
		"steps": [{"action": "screenshot_diff", "with": {"name": "menu.1080p"}}]
	}`)); err != nil {
		t.Errorf("Parse rejected a safe baseline name: %v", err)
	}
}

func TestParseRejectsBlockedMethodCall(t *testing.T) {
	_, err := Parse([]byte(`{
		"target": {"mode": "launch", "project_path": "p"},
		"steps": [{"action": "call_method", "with": {"selector": "name:X", "method": "queue_free"}}]
	}`))
	if err == nil {
		t.Fatal("Parse accepted a call to a blocked method")
	}
}

func TestParseRejectsEmptyScenario(t *testing.T) {
	if _, err := Parse([]byte(`{"target": {"mode": "launch", "project_path": "p"}, "steps": []}`)); err == nil {
		t.Fatal("Parse accepted a scenario with no steps")
	}
}

func TestActionsIncludeLocalAndProtocolActions(t *testing.T) {
	actions := Actions()
	for _, want := range []string{"sleep", "assert_property", "assert_node_count", "screenshot_diff", "click", "wait_for_node"} {
		if !contains(actions, want) {
			t.Errorf("Actions() is missing %q", want)
		}
	}
}

// TestActionsHasNoDuplicates guards the whole merged action set (local +
// GWP), not just screenshot: a future double-registration of any action name
// must fail this test the same way screenshot's did.
func TestActionsHasNoDuplicates(t *testing.T) {
	actions := Actions()
	seen := make(map[string]int, len(actions))
	for _, action := range actions {
		seen[action]++
	}
	for action, count := range seen {
		if count > 1 {
			t.Errorf("Actions() lists %q %d times, want 1", action, count)
		}
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
