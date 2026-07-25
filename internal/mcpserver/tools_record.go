package mcpserver

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

var recordStartTool = mcp.NewTool("godot_record_start",
	mcp.WithDescription("Start recording player input in the running Godot game"),
	mcp.WithString("output_path",
		mcp.Description("File path where the recording will be saved, e.g. \"res://recordings/run1.json\". Defaults to a timestamped file under user://stagehand_recordings/."),
	),
	mcp.WithBoolean("include_mouse_move",
		mcp.Description("Capture mouse-motion events too. Off by default: motion dominates a recording's size and is rarely what a repro depends on."),
		mcp.DefaultBool(false),
	),
	instanceIDOpt,
)

func (s *Server) handleRecordStart(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	instanceID := req.GetString("instance_id", "default")
	params := map[string]any{
		"include_mouse_move": req.GetBool("include_mouse_move", false),
	}
	// Forwarded only when set — an empty output_path means "addon picks one",
	// and the addon distinguishes absent from empty.
	if outputPath := req.GetString("output_path", ""); outputPath != "" {
		params["output_path"] = outputPath
	}

	result, errResult := s.callGodotInstance(ctx, instanceID, "record_start", params)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}

var recordStopTool = mcp.NewTool("godot_record_stop",
	mcp.WithDescription("Stop an active recording in the running Godot game and write it to disk"),
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
	mcp.WithString("recording_path",
		mcp.Description("File path of the recording to replay, e.g. \"res://recordings/run1.json\""),
	),
	mcp.WithString("input_path",
		mcp.Description("Deprecated alias for recording_path"),
	),
	mcp.WithNumber("speed",
		mcp.Description("Playback speed multiplier. 2.0 replays twice as fast; use it to shorten CI runs. Must be greater than 0."),
		mcp.DefaultNumber(1),
	),
	mcp.WithBoolean("wait_for_ready",
		mcp.Description("Wait for the current scene to finish loading before injecting the first event."),
		mcp.DefaultBool(true),
	),
	instanceIDOpt,
)

func (s *Server) handleReplay(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	instanceID := req.GetString("instance_id", "default")

	// `input_path` is the pre-vrj.6 name, kept working for callers and saved
	// scripts written against the shipped tool.
	recordingPath := req.GetString("recording_path", "")
	if recordingPath == "" {
		recordingPath = req.GetString("input_path", "")
	}
	if recordingPath == "" {
		return mcp.NewToolResultError(`required argument "recording_path" not found`), nil
	}

	speed := req.GetFloat("speed", 1.0)
	if speed <= 0 {
		return mcp.NewToolResultError(fmt.Sprintf("speed must be greater than 0, got %v", speed)), nil
	}

	// The wire parameter stays `input_path` so a newer binary still drives an
	// addon build from before the rename.
	result, errResult := s.callGodotInstance(ctx, instanceID, "replay", map[string]any{
		"input_path":     recordingPath,
		"speed":          speed,
		"wait_for_ready": req.GetBool("wait_for_ready", true),
	})
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}
