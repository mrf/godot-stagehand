package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mrf/godot-stagehand/internal/godotconn"
)

// Server wraps an MCP server and manages the Godot connection.
type Server struct {
	mcp *server.MCPServer

	mu   sync.RWMutex
	conn *godotconn.Connection
}

// New creates a new MCP server with all Godot tools registered.
func New() *Server {
	s := &Server{}

	s.mcp = server.NewMCPServer(
		"godot-stagehand",
		"0.1.0",
		server.WithDescription("Automate and test running Godot games from external processes"),
	)

	s.registerTools()
	return s
}

// Serve runs the MCP server over stdio with signal-based graceful shutdown.
func (s *Server) Serve() error {
	return server.ServeStdio(s.mcp)
}

// setConn stores a new Godot connection.
func (s *Server) setConn(conn *godotconn.Connection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conn = conn
}

// getConn returns the current Godot connection, or nil.
func (s *Server) getConn() *godotconn.Connection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.conn
}

// requireConn returns the connection or an MCP error result if not connected.
func (s *Server) requireConn() (*godotconn.Connection, *mcp.CallToolResult) {
	conn := s.getConn()
	if conn == nil {
		return nil, mcp.NewToolResultError(
			"Not connected. Call godot_connect or godot_launch first.",
		)
	}
	return conn, nil
}

// callGodot sends a JSON-RPC method to the Godot addon and returns the raw result.
func (s *Server) callGodot(ctx context.Context, method string, params any) (json.RawMessage, *mcp.CallToolResult) {
	conn, errResult := s.requireConn()
	if errResult != nil {
		return nil, errResult
	}
	resp, err := conn.Call(ctx, method, params)
	if err != nil {
		return nil, mcp.NewToolResultError(fmt.Sprintf("Godot error: %v", err))
	}
	return resp.Result, nil
}

func (s *Server) registerTools() {
	s.mcp.AddTool(connectTool, s.handleConnect)
	s.mcp.AddTool(getGameStateTool, s.handleGetGameState)
	s.mcp.AddTool(getTreeTool, s.handleGetTree)
	s.mcp.AddTool(findNodesTool, s.handleFindNodes)
	s.mcp.AddTool(getPropertyTool, s.handleGetProperty)
	s.mcp.AddTool(setPropertyTool, s.handleSetProperty)
	s.mcp.AddTool(clickTool, s.handleClick)
	s.mcp.AddTool(pressKeyTool, s.handlePressKey)
	s.mcp.AddTool(pressActionTool, s.handlePressAction)
	s.mcp.AddTool(screenshotTool, s.handleScreenshot)
}
