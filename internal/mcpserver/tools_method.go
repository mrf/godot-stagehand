package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// blockedMethods lists Godot methods that must not be called remotely.
// The GDScript side enforces the same list; this is defense-in-depth.
var blockedMethods = map[string]bool{
	"free":                   true,
	"queue_free":             true,
	"set_script":             true,
	"add_child":              true,
	"remove_child":           true,
	"queue_redraw":           true,
	"notification":           true,
	"propagate_notification": true,
	"set_process":            true,
	"set_physics_process":    true,
}

// validateMethod returns an error message if the method is blocked, or empty string if allowed.
func validateMethod(method string) string {
	if strings.HasPrefix(method, "_") {
		return "Blocked: private/lifecycle methods (starting with '_') cannot be called"
	}
	if blockedMethods[method] {
		return fmt.Sprintf("Blocked: '%s' is a destructive method and cannot be called remotely", method)
	}
	return ""
}

var changeSceneTool = mcp.NewTool("godot_change_scene",
	mcp.WithDescription("Change to a different scene in the running Godot game"),
	mcp.WithDestructiveHintAnnotation(true),
	mcp.WithString("scene_path",
		mcp.Required(),
		mcp.Description("Resource path to the new scene, e.g. \"res://scenes/main_menu.tscn\""),
	),
)

func (s *Server) handleChangeScene(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	scenePath, err := req.RequireString("scene_path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result, errResult := s.callGodot(ctx, "change_scene", map[string]any{
		"scene_path": scenePath,
	})
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}

var callMethodTool = mcp.NewTool("godot_call_method",
	mcp.WithDescription("Call a method on a Godot node. Some destructive and private methods are blocked for safety."),
	mcp.WithDestructiveHintAnnotation(true),
	mcp.WithString("selector",
		mcp.Required(),
		mcp.Description("Target node selector"),
	),
	mcp.WithString("method",
		mcp.Required(),
		mcp.Description("Method name to call"),
	),
	mcp.WithArray("args",
		mcp.Description("Arguments to pass to the method"),
	),
	mcp.WithBoolean("allow_multiple",
		mcp.Description("Allow calling on multiple matched nodes"),
		mcp.DefaultBool(false),
	),
)

func (s *Server) handleCallMethod(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	selector, err := req.RequireString("selector")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	method, err := req.RequireString("method")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if errMsg := validateMethod(method); errMsg != "" {
		return mcp.NewToolResultError(errMsg), nil
	}

	if errResult := validateSelector(selector); errResult != nil {
		return errResult, nil
	}

	params := map[string]any{
		"selector": selector,
		"method":   method,
	}
	if args, ok := req.GetArguments()["args"]; ok {
		params["args"] = args
	}
	if allowMultiple := req.GetBool("allow_multiple", false); allowMultiple {
		params["allow_multiple"] = allowMultiple
	}

	result, errResult := s.callGodot(ctx, "call_method", params)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}

var evaluateTool = mcp.NewTool("godot_evaluate",
	mcp.WithDescription("Evaluate a GDScript expression in the running Godot game. Engine singletons (Engine, OS, Time, Input, DisplayServer, ProjectSettings, …) are available. NOTE: conditional/ternary expressions ('a if c else b') are NOT supported by Godot's Expression parser — split them into separate calls. DANGEROUS — only works when Stagehand is enabled."),
	mcp.WithDestructiveHintAnnotation(true),
	mcp.WithString("expression",
		mcp.Required(),
		mcp.Description("GDScript expression to evaluate"),
	),
	mcp.WithString("context_node",
		mcp.Description("Optional node path to use as 'self' context"),
	),
)

func (s *Server) handleEvaluate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	expression, err := req.RequireString("expression")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	params := map[string]any{
		"expression": expression,
	}
	if ctxNode := req.GetString("context_node", ""); ctxNode != "" {
		if errResult := validateSelector(ctxNode); errResult != nil {
			return errResult, nil
		}
		params["context_node"] = ctxNode
	}

	result, errResult := s.callGodot(ctx, "evaluate", params)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}
