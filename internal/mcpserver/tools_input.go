package mcpserver

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

var clickTool = mcp.NewTool("godot_click",
	mcp.WithDescription("Click on a node or at screen coordinates in the Godot game"),
	mcp.WithString("selector",
		mcp.Description("Node to click, supports selectors like \"class:Button\", \"name:OkBtn\", \"text:Submit\", \"meta:id=confirm\", or \"div >> text:Submit\""),
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

var typeTextTool = mcp.NewTool("godot_type_text",
	mcp.WithDescription("Send text input to the focused control in the Godot game"),
	mcp.WithString("text",
		mcp.Required(),
		mcp.Description("Text to type"),
	),
	mcp.WithNumber("delay_ms",
		mcp.Description("Delay between characters in milliseconds"),
		mcp.DefaultNumber(50),
		mcp.Min(0),
	),
	mcp.WithString("selector",
		mcp.Description("Optional node to click first to gain focus"),
	),
)

func (s *Server) handleTypeText(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	text, err := req.RequireString("text")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	params := map[string]any{
		"text":     text,
		"delay_ms": req.GetInt("delay_ms", 50),
	}
	
	if selector, hasSelector := req.GetArguments()["selector"]; hasSelector {
		params["selector"] = selector
	}

	result, errResult := s.callGodot(ctx, "input_text", params)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}

var mouseMoveTool = mcp.NewTool("godot_mouse_move",
	mcp.WithDescription("Move mouse cursor to a specific position without clicking"),
	mcp.WithString("selector",
		mcp.Description("Node whose center to move mouse to"),
	),
	mcp.WithObject("coordinates",
		mcp.Description("Absolute screen coordinates {x, y} to move mouse to"),
		mcp.Properties(map[string]any{
			"x": map[string]interface{}{"type": "number", "description": "X coordinate"},
			"y": map[string]interface{}{"type": "number", "description": "Y coordinate"},
		}),
	),
)

func (s *Server) handleMouseMove(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	selector, hasSelector := args["selector"]
	coords, hasCoords := args["coordinates"]

	if !hasSelector && !hasCoords {
		return mcp.NewToolResultError("one of 'selector' or 'coordinates' is required"), nil
	}
	
	params := map[string]interface{}{}
	
	if hasSelector {
		params["selector"] = selector
	}
	if hasCoords {
		params["coordinates"] = coords
	}

	result, errResult := s.callGodot(ctx, "input_mouse_move", params)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}
