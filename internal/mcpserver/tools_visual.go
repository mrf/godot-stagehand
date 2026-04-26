package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

var screenshotTool = mcp.NewTool("godot_screenshot",
	mcp.WithDescription("Capture a screenshot of the Godot game viewport"),
	mcp.WithReadOnlyHintAnnotation(true),
	mcp.WithString("selector",
		mcp.Description("Crop the screenshot to this node's bounding rect"),
	),
	mcp.WithBoolean("full_page",
		mcp.Description("Capture the full viewport"),
		mcp.DefaultBool(true),
	),
)

// screenshotResult is the expected shape of the Godot screenshot response.
type screenshotResult struct {
	Data     string `json:"data"`
	MimeType string `json:"mime_type"`
}

func (s *Server) handleScreenshot(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	params := map[string]any{
		"full_page": req.GetBool("full_page", true),
	}
	if sel := req.GetString("selector", ""); sel != "" {
		params["selector"] = sel
	}

	raw, errResult := s.callGodot(ctx, "screenshot", params)
	if errResult != nil {
		return errResult, nil
	}

	var sr screenshotResult
	if err := json.Unmarshal(raw, &sr); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to parse screenshot response: %v", err)), nil
	}
	if sr.MimeType == "" {
		sr.MimeType = "image/png"
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewImageContent(sr.Data, sr.MimeType),
		},
	}, nil
}
