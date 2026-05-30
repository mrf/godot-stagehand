package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mrf/godot-stagehand/internal/godotconn"
	"github.com/mrf/godot-stagehand/internal/launch"
	"github.com/mrf/godot-stagehand/internal/selector"
)

// Server wraps an MCP server and manages the Godot connection.
type Server struct {
	mcp *server.MCPServer

	mu   sync.RWMutex
	conn *godotconn.Connection

	muLaunch     sync.RWMutex
	launchResult *launch.LaunchResult

	// baselineDir is the directory where screenshot baselines are stored.
	baselineDir string
}

// New creates a new MCP server with all Godot tools registered.
func New() *Server {
	s := &Server{
		baselineDir: "stagehand-baselines",
	}

	s.mcp = server.NewMCPServer(
		"godot-stagehand",
		"0.1.0",
		server.WithDescription("Automate and test running Godot games from external processes"),
	)

	s.registerTools()
	return s
}

// Serve runs the MCP server over stdio with signal-based graceful shutdown.
func (s *Server) cleanup() {
	s.killExistingLaunch()
	s.clearConn()
}

func (s *Server) Serve() error {
	defer s.cleanup()
	return server.ServeStdio(s.mcp)
}

// setConn stores a new Godot connection.
func (s *Server) setConn(conn *godotconn.Connection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conn = conn
}

// clearConn closes and clears the current Godot connection.
func (s *Server) clearConn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
}

// getConn returns the current Godot connection, or nil.
func (s *Server) getConn() *godotconn.Connection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.conn
}

// setLaunchResult stores the result of a successful godot_launch.
func (s *Server) setLaunchResult(result *launch.LaunchResult) {
	s.muLaunch.Lock()
	defer s.muLaunch.Unlock()
	s.launchResult = result
}

// getLaunchResult returns the current launch result, or nil.
func (s *Server) getLaunchResult() *launch.LaunchResult {
	s.muLaunch.RLock()
	defer s.muLaunch.RUnlock()
	return s.launchResult
}

// killExistingLaunch kills any previously launched Godot process and clears the launch result.
func (s *Server) killExistingLaunch() {
	s.muLaunch.Lock()
	defer s.muLaunch.Unlock()
	if s.launchResult != nil {
		// Close the connection first.
		if s.launchResult.Conn != nil {
			s.launchResult.Conn.Close()
		}
		// Kill the process.
		_ = s.launchResult.Kill()
		s.launchResult = nil
	}
}

// validateSelector parses and validates a selector string via ParseChain.
// Returns an MCP error result if invalid, nil if valid.
func validateSelector(sel string) *mcp.CallToolResult {
	if _, err := selector.ParseChain(sel); err != nil {
		return mcp.NewToolResultError("invalid selector: " + err.Error())
	}
	return nil
}

// toolResultToError extracts the text from an MCP error result and returns it
// as a Go error. Used by internal helpers that need to convert MCP-style errors
// into standard errors.
func toolResultToError(result *mcp.CallToolResult, fallback string) error {
	if len(result.Content) > 0 {
		if tc, ok := mcp.AsTextContent(result.Content[0]); ok {
			return fmt.Errorf("%s", tc.Text)
		}
	}
	return fmt.Errorf("%s", fallback)
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

// checkGodotResult inspects a raw JSON result for a top-level "error" key.
// The GDScript addon returns handler-level errors as {"error": "..."} in an
// otherwise successful JSON-RPC response. This helper surfaces them as MCP
// errors so callers get a clear message instead of a confusing decode failure.
// Returns nil when no error key is present.
func checkGodotResult(raw json.RawMessage) *mcp.CallToolResult {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	errVal, ok := m["error"]
	if !ok {
		return nil
	}
	var payload struct {
		Error     string         `json:"error"`
		ErrorCode string         `json:"error_code"`
		Details   map[string]any `json:"details"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.Error == "" {
		var errMsg string
		if err := json.Unmarshal(errVal, &errMsg); err == nil {
			return mcp.NewToolResultError(errMsg)
		}
		return mcp.NewToolResultError("godot handler error (unparseable)")
	}
	return mcp.NewToolResultError(formatGodotError(payload.Error, payload.ErrorCode, payload.Details))
}

func formatGodotError(message string, code string, detailsMap map[string]any) string {
	if code != "" {
		message += fmt.Sprintf(" (code=%s)", code)
	}
	if len(detailsMap) > 0 {
		details, err := json.Marshal(detailsMap)
		if err == nil {
			message += fmt.Sprintf(" details=%s", details)
		}
	}
	return message
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
	if errResult := checkGodotResult(resp.Result); errResult != nil {
		return nil, errResult
	}
	return resp.Result, nil
}

func (s *Server) registerTools() {
	s.mcp.AddTool(connectTool, s.handleConnect)
	s.mcp.AddTool(launchTool, s.handleLaunch)
	s.mcp.AddTool(statusTool, s.handleStatus)
	s.mcp.AddTool(getGameStateTool, s.handleGetGameState)
	s.mcp.AddTool(getTreeTool, s.handleGetTree)
	s.mcp.AddTool(findNodesTool, s.handleFindNodes)
	s.mcp.AddTool(getPropertyTool, s.handleGetProperty)
	s.mcp.AddTool(setPropertyTool, s.handleSetProperty)
	s.mcp.AddTool(clickTool, s.handleClick)
	s.mcp.AddTool(pressKeyTool, s.handlePressKey)
	s.mcp.AddTool(pressActionTool, s.handlePressAction)
	s.mcp.AddTool(touchTool, s.handleTouch)
	s.mcp.AddTool(typeTextTool, s.handleTypeText)
	s.mcp.AddTool(mouseMoveTool, s.handleMouseMove)
	s.mcp.AddTool(screenshotTool, s.handleScreenshot)
	s.mcp.AddTool(saveBaselineTool, s.handleSaveBaseline)
	s.mcp.AddTool(screenshotDiffTool, s.handleScreenshotDiff)
	s.mcp.AddTool(waitForNodeTool, s.handleWaitForNode)
	s.mcp.AddTool(waitForSignalTool, s.handleWaitForSignal)
	s.mcp.AddTool(waitForPropertyTool, s.handleWaitForProperty)
	s.mcp.AddTool(changeSceneTool, s.handleChangeScene)
	s.mcp.AddTool(callMethodTool, s.handleCallMethod)
	s.mcp.AddTool(evaluateTool, s.handleEvaluate)
	s.mcp.AddTool(getPerformanceTool, s.handleGetPerformance)
	s.mcp.AddTool(assertPerformanceTool, s.handleAssertPerformance)
	s.mcp.AddTool(recordStartTool, s.handleRecordStart)
	s.mcp.AddTool(recordStopTool, s.handleRecordStop)
	s.mcp.AddTool(replayTool, s.handleReplay)
}
