package mcpserver

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mrf/godot-stagehand/internal/godotconn"
)

var connectTool = mcp.NewTool("godot_connect",
	mcp.WithDescription("Connect to a running Godot game with the stagehand addon enabled"),
	mcp.WithString("host",
		mcp.Description("WebSocket host"),
		mcp.DefaultString("localhost"),
	),
	mcp.WithNumber("port",
		mcp.Description("WebSocket port"),
		mcp.DefaultNumber(26700),
	),
)

func (s *Server) handleConnect(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	host := req.GetString("host", "localhost")
	port := req.GetInt("port", 26700)

	// Close any existing connection.
	if existing := s.getConn(); existing != nil {
		existing.Close()
	}

	conn, err := godotconn.Dial(ctx, host, port)
	if err != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("Failed to connect to Godot at %s:%d: %v", host, port, err),
		), nil
	}
	s.setConn(conn)

	// Ping to verify the connection and get engine info.
	result, errResult := s.callGodot(ctx, "ping", nil)
	if errResult != nil {
		return errResult, nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Connected to Godot at %s:%d\n%s", host, port, string(result))), nil
}

var getGameStateTool = mcp.NewTool("godot_get_game_state",
	mcp.WithDescription("Get the current game state: scene, FPS, physics state, window size"),
	mcp.WithReadOnlyHintAnnotation(true),
)

func (s *Server) handleGetGameState(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	result, errResult := s.callGodot(ctx, "get_game_state", nil)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}
