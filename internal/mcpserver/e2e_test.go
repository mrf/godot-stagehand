package mcpserver

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mrf/godot-stagehand/internal/godotconn"
)

var upgrader = websocket.Upgrader{}

// stubGodot is a mock Godot addon WebSocket server that responds to GWP methods
// with canned responses.
type stubGodot struct {
	*httptest.Server
	testServerConn *websocket.Conn
}

func newStubGodot(t testing.TB) *stubGodot {
	s := &stubGodot{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		s.testServerConn = ws
		defer ws.Close()
		for {
			var req godotconn.Request
			if err := ws.ReadJSON(&req); err != nil {
				return
			}
			resp := s.handleReq(req)
			if err := ws.WriteJSON(resp); err != nil {
				return
			}
		}
	}))
	return s
}

func (s *stubGodot) handleReq(req godotconn.Request) godotconn.Response {
	resp := godotconn.Response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "ping":
		resp.Result = rawJSON(`{"status":"ok","engine":"godot","engine_version":"4.2.1"}`)
	case "get_tree":
		resp.Result = rawJSON(`{"name":"root","class":"Node","path":"/root","children":[{"name":"UI","class":"CanvasLayer","path":"/root/UI","children":[{"name":"StartButton","class":"Button","path":"/root/UI/StartButton"}]}],"count":3}`)
	case "query_nodes":
		resp.Result = rawJSON(`{"nodes":[{"name":"StartButton","class":"Button","path":"/root/UI/StartButton"}],"count":1}`)
	case "get_property":
		resp.Result = rawJSON(`{"value":"Start Game","type":"String"}`)
	case "set_property":
		resp.Result = rawJSON(`{"success":true,"previous_value":"Old Text"}`)
	case "get_game_state":
		resp.Result = rawJSON(`{"current_scene":"res://main.tscn","fps":60,"physics_ticks":120,"window_size":{"x":1280,"y":720},"connected":true,"engine_version":"4.2.1"}`)
	case "input_mouse", "input_action", "input_key":
		resp.Result = rawJSON(`{"success":true}`)
	case "screenshot":
		resp.Result = rawJSON(`{"data":"iVBORw0KGgo=","mime_type":"image/png","width":1280,"height":720}`)
	default:
		resp.Error = &godotconn.RPCError{Code: godotconn.CodeMethodNotFound, Message: "unknown method: " + req.Method}
	}
	return resp
}

func rawJSON(s string) []byte { return []byte(s) }

func serverHostPort(t testing.TB, srv *httptest.Server) (string, int) {
	addr := srv.Listener.Addr().String()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}

func TestE2E_ConnectAndGetFullTree(t *testing.T) {
	stub := newStubGodot(t)
	defer stub.Close()
	host, port := serverHostPort(t, stub.Server)

	mcp := New()
	ctx := context.Background()

	// Connect to the mock Godot server.
	req := toolReq(map[string]any{
		"host": host,
		"port": float64(port),
	})
	result, err := mcp.handleConnect(ctx, req)
	if err != nil {
		t.Fatalf("handleConnect: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.IsError {
		t.Fatalf("connection failed: %+v", result)
	}

	// Get tree.
	result, err = mcp.handleGetTree(ctx, toolReq(map[string]any{
		"root_path": "/root",
		"max_depth": 10,
	}))
	if err != nil {
		t.Fatalf("handleGetTree: %v", err)
	}
	if result.IsError {
		t.Fatalf("get_tree error: %+v", result)
	}
	text := mustText(t, result)
	if !strings.Contains(text, "StartButton") {
		t.Errorf("get_tree result missing StartButton: %s", text)
	}
}

func TestE2E_FindNodes(t *testing.T) {
	stub := newStubGodot(t)
	defer stub.Close()
	host, port := serverHostPort(t, stub.Server)

	mcp := New()
	ctx := context.Background()
	mcp.handleConnect(ctx, toolReq(map[string]any{"host": host, "port": float64(port)}))

	result, err := mcp.handleFindNodes(ctx, toolReq(map[string]any{
		"selector": "class:Button",
	}))
	if err != nil {
		t.Fatalf("handleFindNodes: %v", err)
	}
	if result.IsError {
		t.Fatalf("find_nodes error: %+v", result)
	}
	text := mustText(t, result)
	if !strings.Contains(text, "StartButton") {
		t.Errorf("find_nodes result missing StartButton: %s", text)
	}
}

func TestE2E_GetAndSetProperty(t *testing.T) {
	stub := newStubGodot(t)
	defer stub.Close()
	host, port := serverHostPort(t, stub.Server)

	mcp := New()
	ctx := context.Background()
	mcp.handleConnect(ctx, toolReq(map[string]any{"host": host, "port": float64(port)}))

	result, err := mcp.handleGetProperty(ctx, toolReq(map[string]any{
		"selector": "class:Button",
		"property": "text",
	}))
	if err != nil {
		t.Fatalf("handleGetProperty: %v", err)
	}
	text := mustText(t, result)
	if !strings.Contains(text, "Start Game") {
		t.Errorf("get_property result: %s", text)
	}

	result, err = mcp.handleSetProperty(ctx, toolReq(map[string]any{
		"selector": "class:Button",
		"property": "text",
		"value":    "New Text",
	}))
	if err != nil {
		t.Fatalf("handleSetProperty: %v", err)
	}
	text = mustText(t, result)
	if !strings.Contains(text, "success") {
		t.Errorf("set_property result: %s", text)
	}
}

func TestE2E_Click(t *testing.T) {
	stub := newStubGodot(t)
	defer stub.Close()
	host, port := serverHostPort(t, stub.Server)

	mcp := New()
	ctx := context.Background()
	mcp.handleConnect(ctx, toolReq(map[string]any{"host": host, "port": float64(port)}))

	result, err := mcp.handleClick(ctx, toolReq(map[string]any{
		"selector": "class:Button",
	}))
	if err != nil {
		t.Fatalf("handleClick: %v", err)
	}
	if result.IsError {
		t.Fatalf("click error: %+v", result)
	}
	text := mustText(t, result)
	if !strings.Contains(text, "success") {
		t.Errorf("click result: %s", text)
	}
}

func TestE2E_PressKey(t *testing.T) {
	stub := newStubGodot(t)
	defer stub.Close()
	host, port := serverHostPort(t, stub.Server)

	mcp := New()
	ctx := context.Background()
	mcp.handleConnect(ctx, toolReq(map[string]any{"host": host, "port": float64(port)}))

	result, err := mcp.handlePressKey(ctx, toolReq(map[string]any{
		"key": "Enter",
	}))
	if err != nil {
		t.Fatalf("handlePressKey: %v", err)
	}
	if result.IsError {
		t.Fatalf("press_key error: %+v", result)
	}
	text := mustText(t, result)
	if !strings.Contains(text, "success") {
		t.Errorf("press_key result: %s", text)
	}
}

func TestE2E_Screenshot(t *testing.T) {
	stub := newStubGodot(t)
	defer stub.Close()
	host, port := serverHostPort(t, stub.Server)

	mcp := New()
	ctx := context.Background()
	mcp.handleConnect(ctx, toolReq(map[string]any{"host": host, "port": float64(port)}))

	result, err := mcp.handleScreenshot(ctx, toolReq(map[string]any{
		"full_page": true,
	}))
	if err != nil {
		t.Fatalf("handleScreenshot: %v", err)
	}
	if result.IsError {
		t.Fatalf("screenshot error: %+v", result)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
}

func TestE2E_GetGameState(t *testing.T) {
	stub := newStubGodot(t)
	defer stub.Close()
	host, port := serverHostPort(t, stub.Server)

	mcp := New()
	ctx := context.Background()
	mcp.handleConnect(ctx, toolReq(map[string]any{"host": host, "port": float64(port)}))

	result, err := mcp.handleGetGameState(ctx, toolReq(nil))
	if err != nil {
		t.Fatalf("handleGetGameState: %v", err)
	}
	if result.IsError {
		t.Fatalf("get_game_state error: %+v", result)
	}
	text := mustText(t, result)
	if !strings.Contains(text, `"fps":60`) {
		t.Errorf("get_game_state missing fps: %s", text)
	}
}

func TestE2E_DisconnectMidSession(t *testing.T) {
	stub := newStubGodot(t)
	defer stub.Close()
	host, port := serverHostPort(t, stub.Server)

	mcp := New()
	ctx := context.Background()
	mcp.handleConnect(ctx, toolReq(map[string]any{"host": host, "port": float64(port)}))

	if stub.testServerConn != nil {
		stub.testServerConn.Close()
	}
	stub.Close()

	result, err := mcp.handleGetTree(ctx, toolReq(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result after disconnect")
	}
}

func toolReq(args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}}
}

func mustText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("no content")
	}
	tc, ok := mcp.AsTextContent(result.Content[0])
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	return tc.Text
}
