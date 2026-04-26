package mcpserver

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

var getTreeTool = mcp.NewTool("godot_get_tree",
	mcp.WithDescription("Get a snapshot of the Godot scene tree"),
	mcp.WithReadOnlyHintAnnotation(true),
	mcp.WithString("root_path",
		mcp.Description("Subtree root path"),
		mcp.DefaultString("/root"),
	),
	mcp.WithNumber("max_depth",
		mcp.Description("Maximum recursion depth"),
		mcp.DefaultNumber(10),
		mcp.Min(1),
		mcp.Max(50),
	),
	mcp.WithArray("include_properties",
		mcp.Description("Property names to include per node"),
		mcp.WithStringItems(),
	),
)

func (s *Server) handleGetTree(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	params := map[string]any{
		"root_path": req.GetString("root_path", "/root"),
		"max_depth": req.GetInt("max_depth", 10),
	}
	if props := req.GetStringSlice("include_properties", nil); len(props) > 0 {
		params["properties"] = props
	}

	result, errResult := s.callGodot(ctx, "get_tree", params)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}

var findNodesTool = mcp.NewTool("godot_find_nodes",
	mcp.WithDescription("Find nodes matching a selector expression (path, name:, class:, group:)"),
	mcp.WithReadOnlyHintAnnotation(true),
	mcp.WithString("selector",
		mcp.Required(),
		mcp.Description("Selector expression, e.g. \"class:Button\", \"name:*Player*\", \"/root/Main\""),
	),
	mcp.WithArray("properties",
		mcp.Description("Property names to return per matched node"),
		mcp.WithStringItems(),
	),
	mcp.WithNumber("limit",
		mcp.Description("Maximum number of results"),
		mcp.DefaultNumber(50),
		mcp.Min(1),
		mcp.Max(500),
	),
)

func (s *Server) handleFindNodes(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	selector, err := req.RequireString("selector")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	params := map[string]any{
		"selector": selector,
		"limit":    req.GetInt("limit", 50),
	}
	if props := req.GetStringSlice("properties", nil); len(props) > 0 {
		params["properties"] = props
	}

	result, errResult := s.callGodot(ctx, "query_nodes", params)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}
