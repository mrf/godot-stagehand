package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mrf/godot-stagehand/internal/launch"
)

var launchTool = mcp.NewTool("godot_launch",
	mcp.WithDescription("Launch a Godot game with the stagehand addon enabled and connect to it"),
	mcp.WithString("project_path",
		mcp.Required(),
		mcp.Description("Path to the Godot project directory (contains project.godot)"),
	),
	mcp.WithString("godot_bin",
		mcp.Description("Path to the Godot binary (default: find via environment or PATH)"),
	),
	mcp.WithString("host",
		mcp.Description(hostSelectionDescription),
		mcp.DefaultString(launch.DefaultHost),
	),
	mcp.WithNumber("port",
		mcp.Description("TCP port for the WebSocket server"),
		mcp.DefaultNumber(26700),
	),
	mcp.WithBoolean("headless",
		mcp.Description("Launch Godot in headless mode"),
		mcp.DefaultBool(true),
	),
	mcp.WithBoolean("expect_screenshots",
		mcp.Description("Set true for screenshot/baseline/diff workflows; rejects headless launches because screenshots need a visible rendered window"),
		mcp.DefaultBool(false),
	),
	mcp.WithArray("extra_args",
		mcp.Description("Extra command-line arguments to pass to the Godot binary"),
		mcp.WithStringItems(),
	),
	mcp.WithNumber("timeout_ms",
		mcp.Description("Maximum time to wait for Godot to start and become ready, in milliseconds"),
		mcp.DefaultNumber(30000),
		mcp.Min(1000),
	),
)

func (s *Server) handleLaunch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectPath, err := req.RequireString("project_path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	godotBin := req.GetString("godot_bin", "")
	host := req.GetString("host", launch.DefaultHost)
	port := req.GetInt("port", 26700)
	headless := req.GetBool("headless", true)
	expectScreenshots := req.GetBool("expect_screenshots", false)
	extraArgs := req.GetStringSlice("extra_args", nil)
	timeoutMs := req.GetInt("timeout_ms", 30000)

	if headless && expectScreenshots {
		return mcp.NewToolResultError("headless=true cannot be used with expect_screenshots=true; relaunch with headless=false and a visible Godot window for godot_screenshot, baselines, and diffs."), nil
	}

	cfg := launch.Config{
		ProjectPath: projectPath,
		GodotBin:    godotBin,
		Host:        host,
		Port:        port,
		Headless:    headless,
		ExtraArgs:   extraArgs,
		TimeoutMs:   timeoutMs,
	}

	// Kill any existing launched process before launching a new one.
	s.killExistingLaunch()

	// Also close any existing connection under the write lock.
	s.clearConn()

	result, err := launch.Launch(ctx, cfg)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to launch Godot: %v", err)), nil
	}

	// Store the launch result for later cleanup.
	s.setLaunchResult(result)

	// Store the connection from the launch result.
	s.setConn(result.Conn)

	// Return structured launch result as JSON.
	jsonResult := map[string]any{
		"pid":                 result.PID,
		"host":                result.Host,
		"port":                result.Port,
		"engine_version":      result.EngineVersion,
		"stagehand_version":   result.StagehandVersion,
		"connection_guidance": connectionGuidance(),
	}
	if headless {
		jsonResult["warnings"] = []string{headlessScreenshotWarning}
	}
	jsonBytes, err := json.MarshalIndent(jsonResult, "", "  ")
	if err != nil {
		// fallback to plain text
		return mcp.NewToolResultText(fmt.Sprintf("Launched Godot (pid=%d) at %s:%d, engine=%s, stagehand=%s",
			result.PID, result.Host, result.Port, result.EngineVersion, result.StagehandVersion)), nil
	}
	return mcp.NewToolResultText(string(jsonBytes)), nil
}
