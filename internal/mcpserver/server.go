package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mrf/godot-stagehand/internal/godotconn"
	"github.com/mrf/godot-stagehand/internal/gwp"
	"github.com/mrf/godot-stagehand/internal/selector"
	"github.com/mrf/godot-stagehand/internal/version"
)

// instanceIDOpt is the shared mcp.ToolOption that adds the optional instance_id
// parameter to every tool that targets a specific Godot connection.
var instanceIDOpt = mcp.WithString("instance_id",
	mcp.Description(`Instance to target (default: "default"). Use distinct IDs to manage multiple simultaneous Godot connections.`),
	mcp.DefaultString("default"),
)

// defaultGodotCallTimeout bounds ordinary Godot RPCs so a silent peer cannot
// occupy an MCP stdio worker forever. Handlers with deliberate longer-running
// semantics, such as wait tools, override it by supplying a context deadline.
const defaultGodotCallTimeout = 30 * time.Second
const maxGodotCallTimeout = 24 * time.Hour

// The stdio transport serves five requests concurrently. Limiting remote Godot
// work to four requests keeps one worker available for local lifecycle tools
// such as status and disconnect when Godot stops responding.
const maxConcurrentGodotCalls = 4

// instanceIDFrom extracts the instance_id from a tool request, defaulting to "default".
func instanceIDFrom(req mcp.CallToolRequest) string {
	return req.GetString("instance_id", "default")
}

// Server wraps an MCP server and manages Godot connections.
type Server struct {
	mcp       *server.MCPServer
	instances *instanceManager

	// baselineDir is the directory where screenshot baselines are stored.
	baselineDir string
	// artifactDir is where failed-diff artifacts are written.
	artifactDir string
	// callTimeout is applied only when the caller did not supply a deadline.
	callTimeout time.Duration
	// callSlots bounds work that can block on a remote Godot process.
	callSlots chan struct{}
}

// New creates a new MCP server with all Godot tools registered.
func New() *Server {
	s := &Server{
		instances:   newInstanceManager(),
		baselineDir: "stagehand-baselines",
		artifactDir: "stagehand-diffs",
		callTimeout: configuredGodotCallTimeout(),
		callSlots:   make(chan struct{}, maxConcurrentGodotCalls),
	}

	s.mcp = server.NewMCPServer(
		"godot-stagehand",
		version.Version,
		server.WithDescription("Automate and test running Godot games from external processes"),
	)

	s.registerTools()
	return s
}

func configuredGodotCallTimeout() time.Duration {
	raw := os.Getenv("STAGEHAND_CALL_TIMEOUT_MS")
	if raw == "" {
		return defaultGodotCallTimeout
	}
	milliseconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || milliseconds <= 0 || milliseconds > int64(maxGodotCallTimeout/time.Millisecond) {
		return defaultGodotCallTimeout
	}
	return time.Duration(milliseconds) * time.Millisecond
}

// Serve runs the MCP server over stdio with signal-based graceful shutdown.
func (s *Server) Serve() error {
	defer s.instances.closeAll()
	return server.ServeStdio(s.mcp)
}

// ── backward-compat wrappers used by tests ────────────────────────────────────

// setConn stores conn as the "default" instance (used by tests).
func (s *Server) setConn(conn *godotconn.Connection) {
	h, p := parseHostPort(conn.Addr())
	s.instances.add("default", h, p, conn, nil)
}

// clearConn closes and removes the "default" instance connection (used by tests).
func (s *Server) clearConn() {
	s.instances.remove("default")
}

// getConn returns the connection for the "default" instance, or nil (used by tests).
func (s *Server) getConn() *godotconn.Connection {
	if e := s.instances.get("default"); e != nil {
		return e.conn
	}
	return nil
}

// ── connection helpers ────────────────────────────────────────────────────────

// requireConnForInstance returns the connection for instanceID, or an MCP error.
func (s *Server) requireConnForInstance(instanceID string) (*godotconn.Connection, *mcp.CallToolResult) {
	e := s.instances.get(instanceID)
	if e == nil {
		if instanceID == "default" {
			return nil, mcp.NewToolResultError("Not connected. Call godot_connect or godot_launch first.")
		}
		return nil, mcp.NewToolResultError(
			fmt.Sprintf("Instance %q not found. Call godot_connect or godot_launch with instance_id=%q first.", instanceID, instanceID),
		)
	}
	return e.conn, nil
}

// ── validation helpers ────────────────────────────────────────────────────────

// validateSelector parses and validates a selector string via ParseChain.
func validateSelector(sel string) *mcp.CallToolResult {
	if _, err := selector.ParseChain(sel); err != nil {
		return mcp.NewToolResultError("invalid selector: " + err.Error())
	}
	return nil
}

// toolResultToError extracts the text from an MCP error result as a Go error.
func toolResultToError(result *mcp.CallToolResult, fallback string) error {
	if len(result.Content) > 0 {
		if tc, ok := mcp.AsTextContent(result.Content[0]); ok {
			return fmt.Errorf("%s", tc.Text)
		}
	}
	return fmt.Errorf("%s", fallback)
}

// checkGodotResult inspects a raw JSON result for a top-level "error" key.
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
	return mcp.NewToolResultError(gwp.FormatError(payload.Error, payload.ErrorCode, payload.Details))
}

// callGodotInstance sends a JSON-RPC method to the named Godot instance.
func (s *Server) callGodotInstance(ctx context.Context, instanceID, method string, params any) (json.RawMessage, *mcp.CallToolResult) {
	conn, errResult := s.requireConnForInstance(instanceID)
	if errResult != nil {
		return nil, errResult
	}
	release, errResult := s.beginGodotCall()
	if errResult != nil {
		return nil, errResult
	}
	defer release()

	callCtx, cancel, appliedTimeout := s.withGodotCallDeadline(ctx)
	defer cancel()

	resp, err := conn.Call(callCtx, method, params)
	if err != nil {
		if appliedTimeout > 0 && errors.Is(err, context.DeadlineExceeded) {
			return nil, mcp.NewToolResultError(
				fmt.Sprintf("Godot call %q timed out after %s", method, appliedTimeout),
			)
		}
		return nil, mcp.NewToolResultError(fmt.Sprintf("Godot error: %v", err))
	}
	if errResult := checkGodotResult(resp.Result); errResult != nil {
		return nil, errResult
	}
	return resp.Result, nil
}

func (s *Server) beginGodotCall() (func(), *mcp.CallToolResult) {
	select {
	case s.callSlots <- struct{}{}:
		return func() { <-s.callSlots }, nil
	default:
		return nil, mcp.NewToolResultError(
			fmt.Sprintf("in-flight Godot call limit (%d) reached; retry after another Godot operation completes", maxConcurrentGodotCalls),
		)
	}
}

func (s *Server) withGodotCallDeadline(ctx context.Context) (context.Context, context.CancelFunc, time.Duration) {
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}, 0
	}
	timeout := s.callTimeout
	if timeout <= 0 {
		timeout = defaultGodotCallTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	return callCtx, cancel, timeout
}

// callGodot sends a JSON-RPC method to the default Godot instance.
func (s *Server) callGodot(ctx context.Context, method string, params any) (json.RawMessage, *mcp.CallToolResult) {
	return s.callGodotInstance(ctx, "default", method, params)
}

// ── tool registration ─────────────────────────────────────────────────────────

func (s *Server) registerTools() {
	s.mcp.AddTool(connectTool, s.handleConnect)
	s.mcp.AddTool(launchTool, s.handleLaunch)
	s.mcp.AddTool(statusTool, s.handleStatus)
	s.mcp.AddTool(listInstancesTool, s.handleListInstances)
	s.mcp.AddTool(disconnectTool, s.handleDisconnect)
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
