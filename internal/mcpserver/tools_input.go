package mcpserver

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

var clickTool = mcp.NewTool("godot_click",
	mcp.WithDescription("Click on a node or at screen coordinates in the Godot game"),
	mcp.WithString("selector",
		mcp.Description("Node to click (uses its center position)"),
	),
	mcp.WithObject("position",
		mcp.Description("Screen coordinates {x, y} to click at"),
		mcp.Properties(map[string]any{
			"x": map[string]any{"type": "number", "description": "X coordinate"},
			"y": map[string]any{"type": "number", "description": "Y coordinate"},
		}),
	),
	mcp.WithString("button",
		mcp.Description("Mouse button"),
		mcp.DefaultString("left"),
		mcp.Enum("left", "right", "middle"),
	),
	mcp.WithBoolean("double_click",
		mcp.Description("Whether to double-click"),
		mcp.DefaultBool(false),
	),
)

func (s *Server) handleClick(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	selector, hasSelector := args["selector"]
	position, hasPosition := args["position"]

	if !hasSelector && !hasPosition {
		return mcp.NewToolResultError("one of 'selector' or 'position' is required"), nil
	}

	params := map[string]any{
		"button":       req.GetString("button", "left"),
		"double_click": req.GetBool("double_click", false),
	}
	if hasSelector {
		params["selector"] = selector
	}
	if hasPosition {
		params["position"] = position
	}

	result, errResult := s.callGodot(ctx, "input_mouse", params)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}

var pressKeyTool = mcp.NewTool("godot_press_key",
	mcp.WithDescription("Simulate a keyboard key press in the Godot game"),
	mcp.WithString("key",
		mcp.Required(),
		mcp.Description("Key name (e.g. \"Enter\", \"Space\", \"W\", \"Escape\")"),
	),
	mcp.WithArray("modifiers",
		mcp.Description("Modifier keys held during the press"),
		mcp.WithStringEnumItems([]string{"shift", "ctrl", "alt", "meta"}),
	),
	mcp.WithNumber("hold_ms",
		mcp.Description("How long to hold the key in milliseconds"),
		mcp.DefaultNumber(100),
		mcp.Min(0),
	),
)

func (s *Server) handlePressKey(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	key, err := req.RequireString("key")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	params := map[string]any{
		"key":     key,
		"hold_ms": req.GetInt("hold_ms", 100),
	}
	if mods := req.GetStringSlice("modifiers", nil); len(mods) > 0 {
		params["modifiers"] = mods
	}

	result, errResult := s.callGodot(ctx, "input_key", params)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}

var pressActionTool = mcp.NewTool("godot_press_action",
	mcp.WithDescription("Simulate a Godot input action (e.g. \"ui_accept\", \"move_left\")"),
	mcp.WithString("action",
		mcp.Required(),
		mcp.Description("Input action name as defined in the Godot project"),
	),
	mcp.WithNumber("strength",
		mcp.Description("Action strength (0.0 to 1.0)"),
		mcp.DefaultNumber(1.0),
		mcp.Min(0),
		mcp.Max(1),
	),
	mcp.WithNumber("hold_ms",
		mcp.Description("How long to hold the action in milliseconds"),
		mcp.DefaultNumber(100),
		mcp.Min(0),
	),
)

func (s *Server) handlePressAction(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	action, err := req.RequireString("action")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	params := map[string]any{
		"action":   action,
		"strength": req.GetFloat("strength", 1.0),
		"hold_ms":  req.GetInt("hold_ms", 100),
	}

	result, errResult := s.callGodot(ctx, "input_action", params)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}
