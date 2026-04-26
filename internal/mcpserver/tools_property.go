package mcpserver

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

var getPropertyTool = mcp.NewTool("godot_get_property",
	mcp.WithDescription("Read a property from a Godot node (supports dot notation like \"position.x\")"),
	mcp.WithReadOnlyHintAnnotation(true),
	mcp.WithString("selector",
		mcp.Required(),
		mcp.Description("Target node selector"),
	),
	mcp.WithString("property",
		mcp.Required(),
		mcp.Description("Property name, supports dot notation (e.g. \"position.x\")"),
	),
)

func (s *Server) handleGetProperty(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	selector, err := req.RequireString("selector")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	property, err := req.RequireString("property")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result, errResult := s.callGodot(ctx, "get_property", map[string]any{
		"selector": selector,
		"property": property,
	})
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}

var setPropertyTool = mcp.NewTool("godot_set_property",
	mcp.WithDescription("Set a property on a Godot node"),
	mcp.WithDestructiveHintAnnotation(true),
	mcp.WithString("selector",
		mcp.Required(),
		mcp.Description("Target node selector"),
	),
	mcp.WithString("property",
		mcp.Required(),
		mcp.Description("Property name"),
	),
	mcp.WithAny("value",
		mcp.Required(),
		mcp.Description("New property value"),
	),
)

func (s *Server) handleSetProperty(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	selector, err := req.RequireString("selector")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	property, err := req.RequireString("property")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	args := req.GetArguments()
	value, ok := args["value"]
	if !ok {
		return mcp.NewToolResultError("missing required argument: value"), nil
	}

	result, errResult := s.callGodot(ctx, "set_property", map[string]any{
		"selector": selector,
		"property": property,
		"value":    value,
	})
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}
