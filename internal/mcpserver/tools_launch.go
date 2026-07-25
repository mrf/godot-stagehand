package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mrf/godot-stagehand/internal/gwp"
	"github.com/mrf/godot-stagehand/internal/launch"
)

// maxAutoPortAttempts bounds the retry loop in launchWithAutoPort. findFreePort
// closes its probe listener before Godot actually binds, so a concurrent
// launch (or any other process) can win the race for that port in the gap; a
// few attempts absorbs that without looping forever on a genuinely exhausted
// port range.
const maxAutoPortAttempts = 3

var launchTool = mcp.NewTool("godot_launch",
	mcp.WithDescription("Launch a Godot game with the stagehand addon enabled and connect to it. "+
		"This is the paved road: the default port 0 auto-assigns a free port, so you get a private instance that no other agent is driving. "+
		"Use this instead of godot_connect unless you must attach to a game someone else already started — godot_connect's default port 26700 is shared and can land you in another agent's SceneTree."),
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
		mcp.Description("TCP port for the WebSocket server (0 = auto-assign a free port, recommended: keeps this instance private to you)"),
		mcp.DefaultNumber(0),
	),
	mcp.WithBoolean("headless",
		mcp.Description("Launch Godot in headless mode"),
		mcp.DefaultBool(true),
	),
	mcp.WithBoolean("expect_screenshots",
		mcp.Description("Set true for screenshot/baseline/diff workflows; rejects headless launches because screenshots need a visible rendered window"),
		mcp.DefaultBool(false),
	),
	mcp.WithBoolean("allow_unsafe",
		mcp.Description("Explicitly enable godot_evaluate and arbitrary godot_call_method requests for this launched session"),
		mcp.DefaultBool(false),
	),
	mcp.WithBoolean("share_user_data",
		mcp.Description("Let this instance use the project's real user:// directory instead of a private, throwaway one. "+
			"Default false: each launch gets an isolated user:// so concurrent instances of one project cannot corrupt each other's saves and settings — "+
			"but nothing written to user:// survives the instance. Set true when you need persistent save data and are running only one instance."),
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
	instanceIDOpt,
)

func (s *Server) handleLaunch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectPath, err := req.RequireString("project_path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	godotBin := req.GetString("godot_bin", "")
	host := req.GetString("host", launch.DefaultHost)
	port := req.GetInt("port", 0)
	headless := req.GetBool("headless", true)
	expectScreenshots := req.GetBool("expect_screenshots", false)
	allowUnsafe := req.GetBool("allow_unsafe", false)
	shareUserData := req.GetBool("share_user_data", false)
	extraArgs := req.GetStringSlice("extra_args", nil)
	timeoutMs := req.GetInt("timeout_ms", 30000)
	instanceID := req.GetString("instance_id", "default")

	if headless && expectScreenshots {
		return mcp.NewToolResultError("headless=true cannot be used with expect_screenshots=true; relaunch with headless=false and a visible Godot window for godot_screenshot, baselines, and diffs."), nil
	}
	release, errResult := s.beginGodotCall()
	if errResult != nil {
		return errResult, nil
	}
	defer release()

	cfg := launch.Config{
		ProjectPath: projectPath,
		GodotBin:    godotBin,
		Host:        host,
		Port:        port,
		Headless:    headless,
		AllowUnsafe: allowUnsafe,
		ExtraArgs:   extraArgs,
		TimeoutMs:   timeoutMs,
		// Isolation is on by default: two agents launching the same project
		// must not share user://, and the shared res://.godot import cache is
		// populated by one serialized headless import before the game starts.
		ShareUserData: shareUserData,
	}

	// Clean up any existing entry for this instanceID before launching.
	s.instances.remove(instanceID)

	var result *launch.LaunchResult
	if port == 0 {
		// Auto-assigned: a lost port race is retryable with a fresh port.
		result, err = launchWithAutoPort(ctx, cfg, findFreePort, launch.Launch)
	} else {
		// Caller pinned an explicit port: a collision is a one-shot, clean error.
		result, err = launch.Launch(ctx, cfg)
	}
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to launch Godot: %v", err)), nil
	}

	// Store the new connection + launch metadata.
	s.instances.add(instanceID, result.Host, result.Port, result.Conn, result)

	jsonResult := map[string]any{
		"instance_id":            instanceID,
		"pid":                    result.PID,
		"host":                   result.Host,
		"port":                   result.Port,
		"engine_version":         result.EngineVersion,
		"stagehand_version":      result.StagehandVersion,
		"unsafe_methods_enabled": allowUnsafe,
		"user_data_dir":          result.UserDataDir,
		"connection_guidance":    connectionGuidance(),
	}
	if result.Handshake != nil {
		jsonResult["protocol"] = gwp.ProtocolID
		jsonResult["capabilities"] = result.Handshake.Capabilities
	}
	var warnings []string
	if headless {
		warnings = append(warnings, headlessScreenshotWarning)
	}
	if result.UserDataWarning != "" {
		warnings = append(warnings, result.UserDataWarning)
	}
	if warning := sharedPortWarning(result.Port); warning != "" {
		warnings = append(warnings, warning)
	}
	// A capability the addon does not serve fails on first use otherwise, with
	// nothing pointing at the addon build as the cause.
	if result.Handshake != nil && len(result.Handshake.MissingOptional) > 0 {
		warnings = append(warnings, result.Handshake.Summary())
	}
	if len(warnings) > 0 {
		jsonResult["warnings"] = warnings
	}
	jsonBytes, err := json.MarshalIndent(jsonResult, "", "  ")
	if err != nil {
		return mcp.NewToolResultText(fmt.Sprintf("Launched Godot (pid=%d) at %s:%d, engine=%s, stagehand=%s",
			result.PID, result.Host, result.Port, result.EngineVersion, result.StagehandVersion)), nil
	}
	return mcp.NewToolResultText(string(jsonBytes)), nil
}

// findFreePort returns a free TCP port on 127.0.0.1.
func findFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// launchWithAutoPort picks a free port and launches Godot on it, retrying up
// to maxAutoPortAttempts times when the previous attempt lost the TOCTOU race
// for the port: pickPort's probe listener closes before Godot actually binds,
// so another process (including a concurrent launch also auto-assigning a
// port) can grab the same one in that window. doLaunch reports this as
// launch.ErrPortUnavailable (see assertPortFree / verifyInstanceToken in the
// launch package); any other error is returned immediately without retrying,
// so a genuinely broken launch (bad project path, missing binary, ...) fails
// clean on the first attempt instead of being masked by this loop.
func launchWithAutoPort(
	ctx context.Context,
	cfg launch.Config,
	pickPort func() (int, error),
	doLaunch func(context.Context, launch.Config) (*launch.LaunchResult, error),
) (*launch.LaunchResult, error) {
	var lastErr error
	for attempt := 0; attempt < maxAutoPortAttempts; attempt++ {
		port, err := pickPort()
		if err != nil {
			return nil, fmt.Errorf("failed to find a free port: %w", err)
		}
		cfg.Port = port
		result, err := doLaunch(ctx, cfg)
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, launch.ErrPortUnavailable) {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("gave up after %d auto-assigned ports all collided with another process: %w", maxAutoPortAttempts, lastErr)
}
