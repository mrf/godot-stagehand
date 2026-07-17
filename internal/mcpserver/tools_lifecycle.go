package mcpserver

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mrf/godot-stagehand/internal/godotconn"
	"github.com/mrf/godot-stagehand/internal/launch"
)

var connectTool = mcp.NewTool("godot_connect",
	mcp.WithDescription("Connect to a running Godot game with the stagehand addon enabled"),
	mcp.WithString("auth_token",
		mcp.Required(),
		mcp.Description("Authentication token for the current Godot session: the token printed at startup when generated, or the configured STAGEHAND_AUTH_TOKEN"),
	),
	mcp.WithString("host",
		mcp.Description(hostSelectionDescription),
		mcp.DefaultString(launch.DefaultHost),
	),
	mcp.WithNumber("port",
		mcp.Description("WebSocket port"),
		mcp.DefaultNumber(26700),
	),
	instanceIDOpt,
)

func (s *Server) handleConnect(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	authToken, err := req.RequireString("auth_token")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	host := req.GetString("host", launch.DefaultHost)
	port := req.GetInt("port", 26700)
	instanceID := req.GetString("instance_id", "default")

	conn, err := godotconn.Dial(ctx, host, port)
	if err != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("Failed to connect to Godot at %s:%d: %v\n\n%s", host, port, err, connectionGuidance()),
		), nil
	}
	if err := conn.Authenticate(ctx, authToken); err != nil {
		_ = conn.Close()
		return mcp.NewToolResultError(
			fmt.Sprintf("Failed to authenticate with Godot at %s:%d: %v", host, port, err),
		), nil
	}

	// Ping to verify the connection and get engine info.
	result, errResult := func() ([]byte, *mcp.CallToolResult) {
		tempEntry := &instanceEntry{conn: conn}
		_ = tempEntry
		resp, err := conn.Call(ctx, "ping", nil)
		if err != nil {
			conn.Close()
			return nil, mcp.NewToolResultError(
				fmt.Sprintf("Failed to connect to Godot at %s:%d: %v\n\n%s", host, port, err, connectionGuidance()),
			)
		}
		if errResult := checkGodotResult(resp.Result); errResult != nil {
			conn.Close()
			return nil, errResult
		}
		return resp.Result, nil
	}()
	if errResult != nil {
		return errResult, nil
	}

	// Replace any existing entry for this instanceID (closes old conn/process).
	s.instances.add(instanceID, host, port, conn, nil)

	return mcp.NewToolResultText(fmt.Sprintf("Connected to Godot at %s:%d (instance_id=%q)\n%s", host, port, instanceID, string(result))), nil
}

var getGameStateTool = mcp.NewTool("godot_get_game_state",
	mcp.WithDescription("Get the current game state: scene, FPS, physics state, window size"),
	mcp.WithReadOnlyHintAnnotation(true),
	instanceIDOpt,
)

func (s *Server) handleGetGameState(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	instanceID := req.GetString("instance_id", "default")
	result, errResult := s.callGodotInstance(ctx, instanceID, "get_game_state", nil)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}
