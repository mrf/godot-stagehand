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
	"free":                     true,
	"queue_free":               true,
	"set_script":               true,
	"add_child":                true,
	"remove_child":             true,
	"queue_redraw":             true,
	"notification":             true,
	"propagate_notification":   true,
	"set_process":              true,
	"set_physics_process":      true,
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

	params := map[string]any{
		"selector": selector,
		"method":   method,
	}
	args := req.GetArguments()
	if a, ok := args["args"]; ok {
		params["args"] = a
	}

	result, errResult := s.callGodot(ctx, "call_method", params)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}

var evaluateTool = mcp.NewTool("godot_evaluate",
	mcp.WithDescription("Evaluate a GDScript expression in the context of a node"),
	mcp.WithDestructiveHintAnnotation(true),
	mcp.WithString("expression",
		mcp.Required(),
		mcp.Description("GDScript expression to evaluate"),
	),
	mcp.WithString("selector",
		mcp.Description("Node selector for the expression context (defaults to scene root)"),
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
	if sel := req.GetString("selector", ""); sel != "" {
		params["selector"] = sel
	}

	result, errResult := s.callGodot(ctx, "evaluate", params)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
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
