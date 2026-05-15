package mcpserver

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

var waitForNodeTool = mcp.NewTool("godot_wait_for_node",
	mcp.WithDescription("Wait for a node matching the selector to reach a desired state"),
	mcp.WithString("selector",
		mcp.Required(),
		mcp.Description("Selector for the node to wait for (path, name:, class:, group:, text:, meta:, unique:, with >> chaining)"),
	),
	mcp.WithString("state",
		mcp.Description("State to wait for: 'exists' (node appears in tree), 'visible' (node appears and is visible), 'removed' (node disappears from tree)"),
		mcp.Enum("exists", "visible", "removed"),
		mcp.DefaultString("exists"),
	),
	mcp.WithNumber("timeout_ms",
		mcp.Description("Maximum time to wait in milliseconds"),
		mcp.DefaultNumber(10000),
		mcp.Min(1),
		mcp.Max(60000),
	),
	mcp.WithNumber("poll_interval_ms",
		mcp.Description("Interval between polls in milliseconds"),
		mcp.DefaultNumber(100),
		mcp.Min(10),
		mcp.Max(5000),
	),
)

func (s *Server) handleWaitForNode(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	selector, err := req.RequireString("selector")
	if err != nil {
		return mcp.NewToolResultError("Invalid 'selector' parameter: " + err.Error()), nil
	}

	params := map[string]any{
		"selector":         selector,
		"state":            req.GetString("state", "exists"),
		"timeout_ms":       req.GetInt("timeout_ms", 10000),
		"poll_interval_ms": req.GetInt("poll_interval_ms", 100),
	}

	result, errResult := s.callGodot(ctx, "wait_for_node", params)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}

var waitForPropertyTool = mcp.NewTool("godot_wait_for_property",
	mcp.WithDescription("Wait for a node's property to satisfy a condition"),
	mcp.WithString("selector",
		mcp.Required(),
		mcp.Description("Selector for the node whose property to wait for"),
	),
	mcp.WithString("property",
		mcp.Required(),
		mcp.Description("Name of the property to monitor"),
	),
	mcp.WithString("operator",
		mcp.Required(),
		mcp.Description("Comparison operator: equals, not_equals, exists, contains, greater_than, less_than"),
		mcp.Enum("equals", "not_equals", "exists", "contains", "greater_than", "less_than"),
	),
	mcp.WithNumber("timeout_ms",
		mcp.Description("Maximum time to wait in milliseconds"),
		mcp.DefaultNumber(10000),
		mcp.Min(1),
		mcp.Max(60000),
	),
	mcp.WithNumber("poll_interval_ms",
		mcp.Description("Interval between polls in milliseconds"),
		mcp.DefaultNumber(100),
		mcp.Min(10),
		mcp.Max(5000),
	),
	mcp.WithAny("expected_value",
		mcp.Description("Value to compare against based on operator"),
	),
)

func (s *Server) handleWaitForProperty(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	selector, err := req.RequireString("selector")
	if err != nil {
		return mcp.NewToolResultError("Invalid 'selector' parameter: " + err.Error()), nil
	}

	property, err := req.RequireString("property")
	if err != nil {
		return mcp.NewToolResultError("Invalid 'property' parameter: " + err.Error()), nil
	}

	operator, err := req.RequireString("operator")
	if err != nil {
		return mcp.NewToolResultError("Invalid 'operator' parameter: " + err.Error()), nil
	}

	params := map[string]any{
		"selector":         selector,
		"property":         property,
		"operator":         operator,
		"timeout_ms":       req.GetInt("timeout_ms", 10000),
		"poll_interval_ms": req.GetInt("poll_interval_ms", 100),
	}

	// Add expected_value if provided
	args := req.GetArguments()
	expVal, hasExpVal := args["expected_value"]
	if hasExpVal {
		params["expected_value"] = expVal
	} else if operator != "exists" {
		// Only required for operators that need comparison
		return mcp.NewToolResultError("parameter 'expected_value' is required for operator '" + operator + "'"), nil
	}

	result, errResult := s.callGodot(ctx, "wait_for_property", params)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}