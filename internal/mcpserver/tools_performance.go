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
	instanceIDOpt,
)

func (s *Server) handleGetPerformance(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	instanceID := req.GetString("instance_id", "default")
	params := map[string]any{}
	if monitors := req.GetStringSlice("monitors", nil); len(monitors) > 0 {
		params["monitors"] = monitors
	}

	result, errResult := s.callGodotInstance(ctx, instanceID, "get_performance", params)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}

var assertPerformanceTool = mcp.NewTool("godot_assert_performance",
	mcp.WithDescription("Sample a Godot performance monitor and assert a chosen statistic against a threshold. "+
		"Returns pass/fail plus the sample count, min, max, mean, median, p95, and engine/render environment metadata. "+
		"Defaults to a single sample (the old instantaneous behavior); set sample_count, duration_ms, or warmup_ms to sample over time."),
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
	mcp.WithString("statistic",
		mcp.Description("Statistic to assert the threshold against"),
		mcp.DefaultString("mean"),
		mcp.Enum("min", "max", "mean", "median", "p95"),
	),
	mcp.WithNumber("warmup_ms",
		mcp.Description("Milliseconds to wait before sampling starts, discarding the engine's initial-frame spike"),
	),
	mcp.WithNumber("sample_count",
		mcp.Description("Number of samples to take (default 1). Mutually exclusive with duration_ms."),
	),
	mcp.WithNumber("sample_interval_ms",
		mcp.Description("Milliseconds between samples (default 16, ~1 frame at 60fps)"),
	),
	mcp.WithNumber("duration_ms",
		mcp.Description("Total sampling duration; sample count is derived from duration_ms / sample_interval_ms. "+
			"Mutually exclusive with sample_count."),
	),
	instanceIDOpt,
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
	instanceID := req.GetString("instance_id", "default")
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
	args := req.GetArguments()
	if _, ok := args["statistic"]; ok {
		params["statistic"] = req.GetString("statistic", "mean")
	}
	if _, ok := args["warmup_ms"]; ok {
		params["warmup_ms"] = req.GetInt("warmup_ms", 0)
	}
	if _, ok := args["sample_interval_ms"]; ok {
		params["sample_interval_ms"] = req.GetInt("sample_interval_ms", 0)
	}
	_, hasSampleCount := args["sample_count"]
	_, hasDuration := args["duration_ms"]
	if hasSampleCount && hasDuration {
		return mcp.NewToolResultError("sample_count and duration_ms are mutually exclusive"), nil
	}
	if hasSampleCount {
		params["sample_count"] = req.GetInt("sample_count", 0)
	}
	if hasDuration {
		params["duration_ms"] = req.GetInt("duration_ms", 0)
	}

	raw, errResult := s.callGodotInstance(ctx, instanceID, "assert_performance", params)
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
