package mcpserver

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

var recordStartTool = mcp.NewTool("godot_record_start",
	mcp.WithDescription("Start recording player input in the running Godot game"),
	mcp.WithString("output_path",
		mcp.Required(),
		mcp.Description("File path where the recording will be saved, e.g. \"res://recordings/run1.json\""),
	),
	instanceIDOpt,
)

func (s *Server) handleRecordStart(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	instanceID := req.GetString("instance_id", "default")
	outputPath, err := req.RequireString("output_path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result, errResult := s.callGodotInstance(ctx, instanceID, "record_start", map[string]any{
		"output_path": outputPath,
	})
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}

var recordStopTool = mcp.NewTool("godot_record_stop",
	mcp.WithDescription("Stop an active recording in the running Godot game"),
	instanceIDOpt,
)

func (s *Server) handleRecordStop(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	instanceID := req.GetString("instance_id", "default")
	result, errResult := s.callGodotInstance(ctx, instanceID, "record_stop", nil)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}

var replayTool = mcp.NewTool("godot_replay",
	mcp.WithDescription("Replay a previously recorded input session in the running Godot game"),
	mcp.WithString("input_path",
		mcp.Required(),
		mcp.Description("File path of the recording to replay, e.g. \"res://recordings/run1.json\""),
	),
	instanceIDOpt,
)

func (s *Server) handleReplay(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	instanceID := req.GetString("instance_id", "default")
	inputPath, err := req.RequireString("input_path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result, errResult := s.callGodotInstance(ctx, instanceID, "replay", map[string]any{
		"input_path": inputPath,
	})
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}
