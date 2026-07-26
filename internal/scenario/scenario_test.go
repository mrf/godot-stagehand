package scenario

import (
	"fmt"
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

func TestParseRejectsOutOfRangePort(t *testing.T) {
	cases := []struct {
		name string
		port int
		ok   bool
	}{
		{"zero", 0, false},
		{"negative", -5, false},
		{"too_high", 65536, false},
		{"way_too_high", 99999, false},
		{"min_boundary", 1, true},
		{"max_boundary", 65535, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := fmt.Sprintf(`{
				"target": {"mode": "connect", "port": %d, "token": "x"},
				"steps": [{"action": "tree"}]
			}`, tc.port)
			_, err := Parse([]byte(src))
			if tc.ok {
				if err != nil {
					t.Fatalf("Parse rejected valid port %d: %v", tc.port, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Parse accepted out-of-range port %d", tc.port)
			}
			if !strings.Contains(err.Error(), "target.port") {
				t.Errorf("error %q does not name target.port", err)
			}
			if !strings.Contains(err.Error(), "1-65535") {
				t.Errorf("error %q does not state the valid range", err)
			}
		})
	}
}

// TestParseRejectsNegativeTimeoutMs pins target.timeout_ms to the same
// non-negative contract as target.port: zero stays valid (it means "use the
// runner's default", per connectDeadline in session.go) but a negative value
// produces an already-expired deadline and must fail at validation time
// instead of surfacing as a mystifying Godot connection timeout.
func TestParseRejectsNegativeTimeoutMs(t *testing.T) {
	cases := []struct {
		name      string
		timeoutMs int
		ok        bool
	}{
		{"negative", -1, false},
		{"zero_means_default", 0, true},
		{"positive", 5000, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := fmt.Sprintf(`{
				"target": {"mode": "launch", "project_path": "p", "timeout_ms": %d},
				"steps": [{"action": "tree"}]
			}`, tc.timeoutMs)
			_, err := Parse([]byte(src))
			if tc.ok {
				if err != nil {
					t.Fatalf("Parse rejected valid timeout_ms %d: %v", tc.timeoutMs, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Parse accepted negative timeout_ms %d", tc.timeoutMs)
			}
			if !strings.Contains(err.Error(), "target.timeout_ms") {
				t.Errorf("error %q does not name target.timeout_ms", err)
			}
			if !strings.Contains(err.Error(), "positive") {
				t.Errorf("error %q does not say it must be positive", err)
			}
		})
	}
}

func TestParseAllowsAutoAssignPortInLaunchMode(t *testing.T) {
	if _, err := Parse([]byte(`{
		"target": {"mode": "launch", "project_path": "p", "port": 0},
		"steps": [{"action": "tree"}]
	}`)); err != nil {
		t.Fatalf("Parse rejected launch mode auto-assign port 0: %v", err)
	}
}

func TestParseRejectsOutOfRangePortInLaunchMode(t *testing.T) {
	_, err := Parse([]byte(`{
		"target": {"mode": "launch", "project_path": "p", "port": 99999},
		"steps": [{"action": "tree"}]
	}`))
	if err == nil {
		t.Fatal("Parse accepted out-of-range port in launch mode")
	}
	if !strings.Contains(err.Error(), "target.port") {
		t.Errorf("error %q does not name target.port", err)
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
