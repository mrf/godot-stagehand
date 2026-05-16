package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mrf/godot-stagehand/internal/godotconn"
)

func TestNew_RegistersAllTools(t *testing.T) {
	s := New()
	tools := s.mcp.ListTools()

	expected := []string{
		"godot_connect",
		"godot_launch",
		"godot_get_game_state",
		"godot_get_tree",
		"godot_find_nodes",
		"godot_get_property",
		"godot_set_property",
		"godot_click",
		"godot_press_key",
		"godot_press_action",
		"godot_touch",
		"godot_type_text",
		"godot_mouse_move",
		"godot_screenshot",
		"godot_screenshot_save_baseline",
		"godot_screenshot_diff",
		"godot_wait_for_node",
		"godot_wait_for_property",
		"godot_wait_for_signal",
		"godot_change_scene",
		"godot_call_method",
		"godot_evaluate",
		"godot_get_performance",
		"godot_assert_performance",
		"godot_record_start",
		"godot_record_stop",
		"godot_replay",
	}

	for _, name := range expected {
		if _, ok := tools[name]; !ok {
			t.Errorf("tool %q not registered", name)
		}
	}

	if len(tools) != len(expected) {
		t.Errorf("expected %d tools, got %d", len(expected), len(tools))
	}
}

func TestToolsReturnErrorWhenNotConnected(t *testing.T) {
	s := New()
	ctx := context.Background()

	tests := []struct {
		name    string
		handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
		args    map[string]any
	}{
		{"godot_get_game_state", s.handleGetGameState, nil},
		{"godot_get_tree", s.handleGetTree, nil},
		{"godot_find_nodes", s.handleFindNodes, map[string]any{"selector": "class:Node"}},
		{"godot_get_property", s.handleGetProperty, map[string]any{"selector": "/root", "property": "name"}},
		{"godot_set_property", s.handleSetProperty, map[string]any{"selector": "/root", "property": "name", "value": "test"}},
		{"godot_click", s.handleClick, map[string]any{"selector": "/root/Button"}},
		{"godot_press_key", s.handlePressKey, map[string]any{"key": "Enter"}},
		{"godot_press_action", s.handlePressAction, map[string]any{"action": "ui_accept"}},
		{"godot_touch", s.handleTouch, map[string]any{"position": map[string]any{"x": float64(100), "y": float64(200)}}},
		{"godot_screenshot", s.handleScreenshot, nil},
		{"godot_wait_for_signal", s.handleWaitForSignal, map[string]any{"selector": "/root/Button", "signal_name": "pressed"}},
		{"godot_call_method", s.handleCallMethod, map[string]any{"selector": "/root", "method": "get_name"}},
		{"godot_evaluate", s.handleEvaluate, map[string]any{"expression": "1+1"}},
		{"godot_get_performance", s.handleGetPerformance, nil},
		{"godot_assert_performance", s.handleAssertPerformance, map[string]any{"monitor": "TIME_FPS", "threshold": float64(60)}},
		{"godot_record_start", s.handleRecordStart, map[string]any{"output_path": "res://recordings/run1.json"}},
		{"godot_record_stop", s.handleRecordStop, nil},
		{"godot_replay", s.handleReplay, map[string]any{"input_path": "res://recordings/run1.json"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			if tt.args != nil {
				req.Params.Arguments = tt.args
			}
			result, err := tt.handler(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.IsError {
				t.Fatal("expected isError=true when not connected")
			}
			text, ok := mcp.AsTextContent(result.Content[0])
			if !ok {
				t.Fatal("expected TextContent")
			}
			if text.Text != "Not connected. Call godot_connect or godot_launch first." {
				t.Errorf("unexpected error text: %s", text.Text)
			}
		})
	}
}

func TestInvalidSelectorReturnsError(t *testing.T) {
	s := New()
	ctx := context.Background()

	// Selectors that are invalid: empty value after a recognized prefix
	invalidSelector := "name:"

	tests := []struct {
		name    string
		handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
		args    map[string]any
	}{
		{"godot_find_nodes", s.handleFindNodes, map[string]any{"selector": invalidSelector}},
		{"godot_get_property", s.handleGetProperty, map[string]any{"selector": invalidSelector, "property": "position"}},
		{"godot_set_property", s.handleSetProperty, map[string]any{"selector": invalidSelector, "property": "position", "value": 0}},
		{"godot_click", s.handleClick, map[string]any{"selector": invalidSelector}},
		{"godot_type_text", s.handleTypeText, map[string]any{"text": "hello", "selector": invalidSelector}},
		{"godot_mouse_move", s.handleMouseMove, map[string]any{"selector": invalidSelector}},
		{"godot_wait_for_node", s.handleWaitForNode, map[string]any{"selector": invalidSelector}},
		{"godot_wait_for_signal", s.handleWaitForSignal, map[string]any{"selector": invalidSelector, "signal_name": "pressed"}},
		{"godot_wait_for_property", s.handleWaitForProperty, map[string]any{"selector": invalidSelector, "property": "x", "operator": "exists"}},
		{"godot_call_method", s.handleCallMethod, map[string]any{"selector": invalidSelector, "method": "get_name"}},
		{"godot_evaluate context_node", s.handleEvaluate, map[string]any{"expression": "1+1", "context_node": invalidSelector}},
		{"godot_screenshot", s.handleScreenshot, map[string]any{"selector": invalidSelector}},
		{"godot_screenshot_save_baseline", s.handleSaveBaseline, map[string]any{"name": "test", "selector": invalidSelector}},
		{"godot_screenshot_diff", s.handleScreenshotDiff, map[string]any{"name": "test", "selector": invalidSelector}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = tt.args
			result, err := tt.handler(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.IsError {
				t.Fatal("expected isError=true for invalid selector")
			}
			text, ok := mcp.AsTextContent(result.Content[0])
			if !ok {
				t.Fatal("expected TextContent")
			}
			if !strings.Contains(text.Text, "invalid selector") {
				t.Errorf("expected 'invalid selector' in error text, got: %s", text.Text)
			}
		})
	}
}

func TestValidSelectorPassesThrough(t *testing.T) {
	s := New()
	ctx := context.Background()

	// These should fail with "not connected", not a selector error
	validSelectors := []string{
		"/root/Main",
		"class:Button",
		"name:OkBtn",
		"group:enemies",
		"name:dialog >> text:Cancel",
	}

	for _, sel := range validSelectors {
		t.Run(sel, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = map[string]any{"selector": sel}
			result, err := s.handleFindNodes(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.IsError {
				t.Fatal("expected isError=true (not connected)")
			}
			text, ok := mcp.AsTextContent(result.Content[0])
			if !ok {
				t.Fatal("expected TextContent")
			}
			if strings.Contains(text.Text, "invalid selector") {
				t.Errorf("valid selector %q incorrectly rejected: %s", sel, text.Text)
			}
		})
	}
}

func TestConnectReturnsErrorForUnreachableHost(t *testing.T) {
	s := New()
	ctx := context.Background()

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"host": "localhost",
		"port": float64(19999), // unlikely to have anything listening
	}
	result, err := s.handleConnect(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected isError=true for unreachable host")
	}
}

// TestClearConnNoRace verifies that concurrent calls to clearConn and getConn
// do not trigger the race detector. This guards against the pattern where
// handleConnect previously called getConn() then Close() without holding any
// lock, creating a window where another goroutine could observe a closing conn.
func TestClearConnNoRace(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	_, portStr, _ := strings.Cut(srv.Listener.Addr().String(), ":")
	port, _ := strconv.Atoi(portStr)

	s := New()
	conn, err := godotconn.Dial(context.Background(), "127.0.0.1", port)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	s.setConn(conn)

	var wg sync.WaitGroup
	const workers = 20
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				s.clearConn()
			} else {
				_ = s.getConn()
			}
		}()
	}
	wg.Wait()
}
