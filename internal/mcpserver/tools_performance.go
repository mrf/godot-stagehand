package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

var getPerformanceTool = mcp.NewTool("godot_get_performance",
	mcp.WithDescription("Get performance metrics from the Godot Performance singleton"),
	mcp.WithReadOnlyHintAnnotation(true),
	mcp.WithArray("monitors",
		mcp.Description("Performance monitor names to query (e.g. TIME_FPS, MEMORY_STATIC). Returns a default set if omitted."),
		mcp.WithStringItems(),
	),
)

func (s *Server) handleGetPerformance(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	params := map[string]any{}
	if monitors := req.GetStringSlice("monitors", nil); len(monitors) > 0 {
		params["monitors"] = monitors
	}

	result, errResult := s.callGodot(ctx, "get_performance", params)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}

var assertPerformanceTool = mcp.NewTool("godot_assert_performance",
	mcp.WithDescription("Assert that a Godot performance monitor value meets a threshold. Returns pass/fail with the actual value."),
	mcp.WithReadOnlyHintAnnotation(true),
	mcp.WithString("monitor",
		mcp.Required(),
		mcp.Description("Performance monitor name (e.g. TIME_FPS, MEMORY_STATIC)"),
	),
	mcp.WithNumber("threshold",
		mcp.Required(),
		mcp.Description("Threshold value to compare against"),
	),
	mcp.WithString("op",
		mcp.Description("Comparison operator: lt (<), lte (<=), gt (>), gte (>=), eq (==)"),
		mcp.DefaultString("lte"),
		mcp.Enum("lt", "lte", "gt", "gte", "eq"),
	),
)

type assertPerformanceResult struct {
	Passed    bool    `json:"passed"`
	Monitor   string  `json:"monitor"`
	Value     float64 `json:"value"`
	Threshold float64 `json:"threshold"`
	Op        string  `json:"op"`
	Message   string  `json:"message,omitempty"`
}

func (s *Server) handleAssertPerformance(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	monitor, err := req.RequireString("monitor")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	threshold, err := req.RequireFloat("threshold")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	op := req.GetString("op", "lte")

	params := map[string]any{
		"monitor":   monitor,
		"threshold": threshold,
		"op":        op,
	}

	raw, errResult := s.callGodot(ctx, "assert_performance", params)
	if errResult != nil {
		return errResult, nil
	}

	var result assertPerformanceResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to parse assert_performance response: %v", err)), nil
	}

	text := string(raw)
	if !result.Passed {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{mcp.NewTextContent(text)},
		}, nil
	}
	return mcp.NewToolResultText(text), nil
}
