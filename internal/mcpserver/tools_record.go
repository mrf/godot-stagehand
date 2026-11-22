package mcpserver

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/mrf/godot-stagehand/internal/gwpop"
)

var recordStartTool = mcp.NewTool("godot_record_start",
	mcp.WithDescription("Start recording player input in the running Godot game"),
	mcp.WithString("output_path",
		mcp.Description("Where to save the recording, inside user://stagehand_recordings/: a relative path such as \"run1.json\" or \"runs/run1.json\", or a full user://stagehand_recordings/... path. res://, absolute and '..' paths are refused. Defaults to a timestamped file in that directory."),
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
		resolved, err := gwpop.ResolveRecordingPath(outputPath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		params["output_path"] = resolved
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
	s.rememberRecordingDuration(instanceID, result)
	return mcp.NewToolResultText(string(result)), nil
}

var replayTool = mcp.NewTool("godot_replay",
	mcp.WithDescription("Replay a previously recorded input session in the running Godot game"),
	mcp.WithString("recording_path",
		mcp.Description("Recording to replay, inside user://stagehand_recordings/: the path record_stop reported, or a relative path such as \"run1.json\". res://, absolute and '..' paths are refused."),
	),
	mcp.WithString("input_path",
		mcp.Description("Deprecated alias for recording_path"),
	),
	mcp.WithNumber("speed",
		mcp.Description("Playback speed multiplier. 2.0 replays twice as fast; use it to shorten CI runs. Between 0.01 and 100."),
		mcp.DefaultNumber(1),
		mcp.Min(minReplaySpeed),
		mcp.Max(maxReplaySpeed),
	),
	mcp.WithBoolean("wait_for_ready",
		mcp.Description("Wait for the current scene to finish loading before injecting the first event."),
		mcp.DefaultBool(true),
	),
	instanceIDOpt,
)

// A near-zero replay speed stretches a recording into an effectively endless
// replay, so it is rejected rather than clamped.
const (
	minReplaySpeed = 0.01
	maxReplaySpeed = 100.0
)

// recordingKey identifies a recording within one Godot instance's user://.
func recordingKey(instanceID, path string) string { return instanceID + "\x00" + path }

// rememberRecordingDuration notes the duration record_stop reported so a later
// replay can derive its deadline. The recording lives in the game's user://,
// which this process cannot read, so record_stop is the only place the length
// is visible; a malformed result is simply not remembered.
func (s *Server) rememberRecordingDuration(instanceID string, recordStopResult json.RawMessage) {
	var stopped struct {
		Path       string  `json:"path"`
		DurationMs float64 `json:"duration_ms"`
	}
	if json.Unmarshal(recordStopResult, &stopped) != nil || stopped.Path == "" {
		return
	}
	s.recordingMu.Lock()
	defer s.recordingMu.Unlock()
	if s.recordingDurations == nil {
		s.recordingDurations = map[string]time.Duration{}
	}
	s.recordingDurations[recordingKey(instanceID, stopped.Path)] = msToDuration(stopped.DurationMs)
}

func (s *Server) recordingDuration(instanceID, path string) (time.Duration, bool) {
	s.recordingMu.Lock()
	defer s.recordingMu.Unlock()
	d, ok := s.recordingDurations[recordingKey(instanceID, path)]
	return d, ok
}

// replayWork is how long the addon spends injecting a recording of the given
// length at the given speed.
func replayWork(recorded time.Duration, speed float64) time.Duration {
	return msToDuration(float64(recorded/time.Millisecond) / speed)
}

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
	recordingPath, err := gwpop.ResolveRecordingPath(recordingPath)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	speed, errResult := boundedNumber(req, "speed", 1.0, minReplaySpeed, maxReplaySpeed)
	if errResult != nil {
		return errResult, nil
	}

	waitForReady := req.GetBool("wait_for_ready", true)
	// The recording's length is known only if this server saw record_stop
	// write it; an unknown recording keeps the ordinary deadline. Waiting for
	// the scene to be ready adds up to one ordinary deadline on top.
	replayCtx := ctx
	if recorded, known := s.recordingDuration(instanceID, recordingPath); known {
		work := replayWork(recorded, speed)
		if waitForReady {
			work += s.ordinaryCallTimeout()
		}
		var cancel context.CancelFunc
		replayCtx, cancel = s.withExpectedWorkDeadline(ctx, work)
		defer cancel()
	}

	// The wire parameter stays `input_path` so a newer binary still drives an
	// addon build from before the rename.
	result, errResult := s.callGodotInstance(replayCtx, instanceID, "replay", map[string]any{
		"input_path":     recordingPath,
		"speed":          speed,
		"wait_for_ready": waitForReady,
	})
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}
