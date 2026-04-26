package mcpserver

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestNew_RegistersAllTools(t *testing.T) {
	s := New()
	tools := s.mcp.ListTools()

	expected := []string{
		"godot_connect",
		"godot_get_game_state",
		"godot_get_tree",
		"godot_find_nodes",
		"godot_get_property",
		"godot_set_property",
		"godot_click",
		"godot_press_key",
		"godot_press_action",
		"godot_screenshot",
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
		{"godot_screenshot", s.handleScreenshot, nil},
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
