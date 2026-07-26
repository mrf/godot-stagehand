package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestNumericParamsEnforceDeclaredBounds is the regression test for
// godot-stagehand-f9o3: mcp-go does not itself enforce the Min/Max declared
// on a tool's number schema, so an out-of-range value previously reached
// deadline math or the Godot wire unchecked. Every case here must be
// rejected with a named-parameter error before any dial is attempted — none
// of these servers have a live Godot connection, so a handler that tries to
// call Godot instead of rejecting the value fails with a connection error,
// not the expected usage error.
func TestNumericParamsEnforceDeclaredBounds(t *testing.T) {
	s := New()
	s.baselineDir = t.TempDir()
	s.artifactDir = t.TempDir()
	ctx := context.Background()

	cases := []struct {
		name    string
		handle  func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
		args    map[string]any
		wantSub string // substring the error text must contain
	}{
		// The exact probe values from the issue report.
		{"wait_for_node timeout_ms negative", s.handleWaitForNode,
			map[string]any{"selector": "/root", "timeout_ms": -5000.0}, "timeout_ms"},
		{"wait_for_node timeout_ms above max", s.handleWaitForNode,
			map[string]any{"selector": "/root", "timeout_ms": 1e15}, "timeout_ms"},
		{"wait_for_node timeout_ms overflow", s.handleWaitForNode,
			map[string]any{"selector": "/root", "timeout_ms": 1e19}, "timeout_ms"},
		{"get_tree max_depth above max", s.handleGetTree,
			map[string]any{"max_depth": 1e6}, "max_depth"},

		// Same class of bug, other handlers/params.
		{"wait_for_node timeout_ms zero (below min)", s.handleWaitForNode,
			map[string]any{"selector": "/root", "timeout_ms": 0.0}, "timeout_ms"},
		{"wait_for_node poll_interval_ms below min", s.handleWaitForNode,
			map[string]any{"selector": "/root", "poll_interval_ms": 5.0}, "poll_interval_ms"},
		{"wait_for_node poll_interval_ms above max", s.handleWaitForNode,
			map[string]any{"selector": "/root", "poll_interval_ms": 999999.0}, "poll_interval_ms"},
		{"wait_for_signal timeout_ms overflow", s.handleWaitForSignal,
			map[string]any{"selector": "/root", "signal_name": "pressed", "timeout_ms": 1e19}, "timeout_ms"},
		{"wait_for_property timeout_ms negative", s.handleWaitForProperty,
			map[string]any{"selector": "/root", "property": "x", "operator": "exists", "timeout_ms": -1.0}, "timeout_ms"},
		{"wait_for_property poll_interval_ms above max", s.handleWaitForProperty,
			map[string]any{"selector": "/root", "property": "x", "operator": "exists", "poll_interval_ms": 6000.0}, "poll_interval_ms"},
		{"get_accessibility_tree max_depth above max", s.handleGetAccessibilityTree,
			map[string]any{"max_depth": 1e6}, "max_depth"},
		{"find_nodes limit zero", s.handleFindNodes,
			map[string]any{"selector": "/root", "limit": 0.0}, "limit"},
		{"find_nodes limit above max", s.handleFindNodes,
			map[string]any{"selector": "/root", "limit": 501.0}, "limit"},
		{"screenshot_diff threshold above max", s.handleScreenshotDiff,
			map[string]any{"name": "stable", "threshold": 1.5}, "threshold"},
		{"screenshot_diff pixel_sensitivity below min", s.handleScreenshotDiff,
			map[string]any{"name": "stable", "pixel_sensitivity": -0.1}, "pixel_sensitivity"},
		{"press_action strength above max", s.handlePressAction,
			map[string]any{"action": "ui_accept", "strength": 1.5}, "strength"},
		{"press_action hold_ms negative", s.handlePressAction,
			map[string]any{"action": "ui_accept", "hold_ms": -5.0}, "hold_ms"},
		{"press_key hold_ms negative", s.handlePressKey,
			map[string]any{"key": "Enter", "hold_ms": -1.0}, "hold_ms"},
		{"touch index above max", s.handleTouch,
			map[string]any{"position": map[string]any{"x": 1.0, "y": 1.0}, "index": 10.0}, "index"},
		{"touch index negative", s.handleTouch,
			map[string]any{"position": map[string]any{"x": 1.0, "y": 1.0}, "index": -1.0}, "index"},
		{"touch duration_ms negative", s.handleTouch,
			map[string]any{"position": map[string]any{"x": 1.0, "y": 1.0}, "duration_ms": -5.0}, "duration_ms"},
		{"type_text delay_ms negative", s.handleTypeText,
			map[string]any{"text": "hi", "delay_ms": -5.0}, "delay_ms"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = tc.args
			res, err := tc.handle(ctx, req)
			if err != nil {
				t.Fatalf("transport error: %v", err)
			}
			if res == nil || !res.IsError {
				t.Fatalf("expected a usage error, got success (or nil result); result=%+v", res)
			}
			text, ok := mcp.AsTextContent(res.Content[0])
			if !ok {
				t.Fatalf("error result has no text content")
			}
			if !strings.Contains(text.Text, tc.wantSub) {
				t.Errorf("error text = %q, want it to mention %q", text.Text, tc.wantSub)
			}
		})
	}
}

// TestConnectPortEnforcesRange is the regression test for
// godot-stagehand-17vv: godot_connect's port had no declared bound and was
// read with plain req.GetInt, so an out-of-range port reached godotconn.Dial
// and came back wrapped in connectionGuidance() — advice about host
// networking, which misdiagnoses a typo'd port number. Out-of-range values
// must be rejected before any dial, with an error that does not mention
// connectionGuidance's host advice.
func TestConnectPortEnforcesRange(t *testing.T) {
	s := New()
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		port float64
		want bool // true = expect rejection
	}{
		{"below range", -1, true},
		{"zero", 0, true},
		{"above range", 65536, true},
		{"valid", 26700, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = map[string]any{
				"auth_token": "token",
				"port":       tc.port,
			}
			res, err := s.handleConnect(ctx, req)
			if err != nil {
				t.Fatalf("transport error: %v", err)
			}
			if !tc.want {
				// A valid port proceeds to dial, which fails in this test
				// environment (no live Godot) — that failure is fine as long
				// as it is a connection error, not a usage rejection.
				if res == nil || !res.IsError {
					t.Fatalf("expected a connection error, got success")
				}
				text, _ := mcp.AsTextContent(res.Content[0])
				if strings.Contains(text.Text, "must be between") {
					t.Errorf("valid port rejected as out-of-range: %q", text.Text)
				}
				return
			}
			if res == nil || !res.IsError {
				t.Fatalf("expected a usage error, got success")
			}
			text, ok := mcp.AsTextContent(res.Content[0])
			if !ok {
				t.Fatalf("error result has no text content")
			}
			if !strings.Contains(text.Text, "port") {
				t.Errorf("error text = %q, want it to mention port", text.Text)
			}
			if strings.Contains(text.Text, "Host guidance") {
				t.Errorf("out-of-range port wrapped in connectionGuidance(): %q", text.Text)
			}
		})
	}
}

// TestLaunchPortEnforcesRange covers godot_launch's port, mirroring
// TestConnectPortEnforcesRange. Port 0 is the auto-assign sentinel and must
// remain valid; it is resolved to a free port before launch.Launch is ever
// called, so it cannot reach the invalid-project-path failure below.
func TestLaunchPortEnforcesRange(t *testing.T) {
	s := New()
	ctx := context.Background()

	for _, port := range []float64{-1, 65536, 99999} {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"project_path": "/nonexistent/project",
			"port":         port,
		}
		res, err := s.handleLaunch(ctx, req)
		if err != nil {
			t.Fatalf("transport error: %v", err)
		}
		if res == nil || !res.IsError {
			t.Fatalf("port %v: expected a usage error, got success", port)
		}
		text, _ := mcp.AsTextContent(res.Content[0])
		if !strings.Contains(text.Text, "port") {
			t.Errorf("port %v: error text = %q, want it to mention port", port, text.Text)
		}
		if strings.Contains(text.Text, "Failed to launch Godot") {
			t.Errorf("port %v: validation ran after attempting to launch, not before: %q", port, text.Text)
		}
	}
}

// TestLaunchTimeoutMsEnforcesBounds covers godot_launch's timeout_ms, which
// has a declared Min but no Max — the bound must still be enforced, and
// enforcement must happen before launch.Launch is ever invoked (no real
// Godot binary is available in this test).
func TestLaunchTimeoutMsEnforcesBounds(t *testing.T) {
	s := New()
	ctx := context.Background()

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"project_path": "/nonexistent/project",
		"timeout_ms":   1e19,
	}
	res, err := s.handleLaunch(ctx, req)
	if err != nil {
		t.Fatalf("transport error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected a usage error for out-of-range timeout_ms, got success")
	}
	text, _ := mcp.AsTextContent(res.Content[0])
	if !strings.Contains(text.Text, "timeout_ms") {
		t.Errorf("error text = %q, want it to mention timeout_ms", text.Text)
	}
	if strings.Contains(text.Text, "Failed to launch Godot") {
		t.Errorf("timeout_ms validation ran after attempting to launch, not before: %q", text.Text)
	}
}
