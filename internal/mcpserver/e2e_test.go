package mcpserver

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mrf/godot-stagehand/internal/godotconn"
)

var upgrader = websocket.Upgrader{}

// stubCall records a single method call received by the stub.
type stubCall struct {
	Method string
	Params json.RawMessage
}

// stubGodot is a mock Godot addon WebSocket server that responds to GWP methods
// with canned responses. It tracks method calls for verification.
type stubGodot struct {
	*httptest.Server
	testServerConn *websocket.Conn

	mu    sync.Mutex
	calls []stubCall
}

func newStubGodot(t *testing.T) *stubGodot {
	t.Helper()
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
			s.recordCall(req)
			resp := s.handleReq(req)
			if err := ws.WriteJSON(resp); err != nil {
				return
			}
		}
	}))
	return s
}

func (s *stubGodot) recordCall(req godotconn.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	params, _ := json.Marshal(req.Params)
	s.calls = append(s.calls, stubCall{Method: req.Method, Params: params})
}

func (s *stubGodot) callCount(method string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.calls {
		if c.Method == method {
			n++
		}
	}
	return n
}

func (s *stubGodot) lastCallParams(method string) json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.calls) - 1; i >= 0; i-- {
		if s.calls[i].Method == method {
			return s.calls[i].Params
		}
	}
	return nil
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
		// Return different value after a click to simulate state change.
		if s.callCount("input_mouse") > 0 {
			resp.Result = rawJSON(`{"value":"Clicked!","type":"String"}`)
		} else {
			resp.Result = rawJSON(`{"value":"Start Game","type":"String"}`)
		}
	case "set_property":
		resp.Result = rawJSON(`{"success":true,"previous_value":"Old Text"}`)
	case "get_game_state":
		resp.Result = rawJSON(`{"current_scene":"res://main.tscn","fps":60,"physics_ticks":120,"window_size":{"x":1280,"y":720},"connected":true,"engine_version":"4.2.1"}`)
	case "wait_for_node":
		nodeState := "exists"
		if p, ok := req.Params.(map[string]any); ok {
			if st, ok := p["state"].(string); ok {
				nodeState = st
			}
		}
		switch nodeState {
		case "removed":
			resp.Result = rawJSON(`{"success":true,"removed":true,"message":"Node removed within timeout period"}`)
		case "visible":
			resp.Result = rawJSON(`{"success":true,"found":true,"visible":true,"message":"Node found and visible within timeout period"}`)
		default:
			resp.Result = rawJSON(`{"success":true,"found":true,"message":"Node found within timeout period"}`)
		}
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

func serverHostPort(t *testing.T, srv *httptest.Server) (string, int) {
	t.Helper()
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

// setupE2ETest creates a stub Godot server, connects an MCP server to it,
// and returns both. Cleanup is registered automatically.
func setupE2ETest(t *testing.T) (*Server, *stubGodot) {
	t.Helper()
	stub := newStubGodot(t)
	t.Cleanup(func() { stub.Close() })

	host, port := serverHostPort(t, stub.Server)
	srv := New()
	ctx := context.Background()

	result, err := srv.handleConnect(ctx, toolReq(map[string]any{
		"host": host,
		"port": float64(port),
	}))
	if err != nil {
		t.Fatalf("setupE2ETest: handleConnect error: %v", err)
	}
	if result.IsError {
		t.Fatalf("setupE2ETest: connection failed: %+v", result)
	}

	return srv, stub
}

func TestE2E_ConnectAndGetFullTree(t *testing.T) {
	srv, _ := setupE2ETest(t)
	ctx := context.Background()

	result, err := srv.handleGetTree(ctx, toolReq(map[string]any{
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
	srv, _ := setupE2ETest(t)
	ctx := context.Background()

	result, err := srv.handleFindNodes(ctx, toolReq(map[string]any{
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
	srv, _ := setupE2ETest(t)
	ctx := context.Background()

	t.Run("GetProperty", func(t *testing.T) {
		result, err := srv.handleGetProperty(ctx, toolReq(map[string]any{
			"selector": "class:Button",
			"property": "text",
		}))
		if err != nil {
			t.Fatalf("handleGetProperty: %v", err)
		}
		if result.IsError {
			t.Fatalf("get_property error: %+v", result)
		}
		text := mustText(t, result)
		if !strings.Contains(text, "Start Game") {
			t.Errorf("expected text to contain %q, got: %s", "Start Game", text)
		}
	})

	t.Run("SetProperty", func(t *testing.T) {
		result, err := srv.handleSetProperty(ctx, toolReq(map[string]any{
			"selector": "class:Button",
			"property": "text",
			"value":    "New Text",
		}))
		if err != nil {
			t.Fatalf("handleSetProperty: %v", err)
		}
		if result.IsError {
			t.Fatalf("set_property error: %+v", result)
		}
		text := mustText(t, result)
		if !strings.Contains(text, "success") {
			t.Errorf("expected text to contain %q, got: %s", "success", text)
		}
	})
}

func TestE2E_Click(t *testing.T) {
	t.Run("ClickBySelector", func(t *testing.T) {
		srv, stub := setupE2ETest(t)
		ctx := context.Background()

		// Verify property before click.
		beforeResult, err := srv.handleGetProperty(ctx, toolReq(map[string]any{
			"selector": "class:Button",
			"property": "text",
		}))
		if err != nil {
			t.Fatalf("handleGetProperty before click: %v", err)
		}
		beforeText := mustText(t, beforeResult)
		if !strings.Contains(beforeText, "Start Game") {
			t.Fatalf("expected %q before click, got: %s", "Start Game", beforeText)
		}

		// Perform click.
		result, err := srv.handleClick(ctx, toolReq(map[string]any{
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
			t.Fatalf("click result missing %q: %s", "success", text)
		}

		// Verify stub received the input_mouse call.
		if n := stub.callCount("input_mouse"); n != 1 {
			t.Errorf("expected 1 input_mouse call, got %d", n)
		}

		// Verify state changed after click.
		afterResult, err := srv.handleGetProperty(ctx, toolReq(map[string]any{
			"selector": "class:Button",
			"property": "text",
		}))
		if err != nil {
			t.Fatalf("handleGetProperty after click: %v", err)
		}
		afterText := mustText(t, afterResult)
		if !strings.Contains(afterText, "Clicked!") {
			t.Errorf("expected property to change after click, got: %s", afterText)
		}
	})

	t.Run("ClickByPosition", func(t *testing.T) {
		srv, _ := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleClick(ctx, toolReq(map[string]any{
			"position": map[string]any{"x": float64(100), "y": float64(200)},
		}))
		if err != nil {
			t.Fatalf("handleClick by position: %v", err)
		}
		if result.IsError {
			t.Fatalf("click by position error: %+v", result)
		}
		text := mustText(t, result)
		if !strings.Contains(text, "success") {
			t.Errorf("click result missing %q: %s", "success", text)
		}
	})

	t.Run("ClickMissingTarget", func(t *testing.T) {
		srv, _ := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleClick(ctx, toolReq(map[string]any{}))
		if err != nil {
			t.Fatalf("handleClick missing target: %v", err)
		}
		if !result.IsError {
			t.Fatal("expected error when neither selector nor position provided")
		}
	})
}

func TestE2E_PressKey(t *testing.T) {
	t.Run("BasicKeyPress", func(t *testing.T) {
		srv, stub := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handlePressKey(ctx, toolReq(map[string]any{
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
			t.Fatalf("press_key result missing %q: %s", "success", text)
		}

		// Verify stub received input_key call with correct params.
		if n := stub.callCount("input_key"); n < 1 {
			t.Fatalf("expected at least 1 input_key call, got %d", n)
		}
		params := stub.lastCallParams("input_key")
		if params == nil {
			t.Fatal("no input_key params recorded")
		}
		var p map[string]any
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatalf("unmarshal input_key params: %v", err)
		}
		if p["key"] != "Enter" {
			t.Errorf("input_key key = %v, want Enter", p["key"])
		}
	})

	t.Run("KeyWithModifiers", func(t *testing.T) {
		srv, stub := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handlePressKey(ctx, toolReq(map[string]any{
			"key":       "S",
			"modifiers": []any{"ctrl"},
		}))
		if err != nil {
			t.Fatalf("handlePressKey with modifiers: %v", err)
		}
		if result.IsError {
			t.Fatalf("press_key with modifiers error: %+v", result)
		}
		text := mustText(t, result)
		if !strings.Contains(text, "success") {
			t.Fatalf("press_key result missing %q: %s", "success", text)
		}

		params := stub.lastCallParams("input_key")
		if params == nil {
			t.Fatal("no input_key params recorded")
		}
		var p map[string]any
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatalf("unmarshal input_key params: %v", err)
		}
		if p["key"] != "S" {
			t.Errorf("input_key key = %v, want S", p["key"])
		}
		mods, ok := p["modifiers"].([]any)
		if !ok || len(mods) == 0 {
			t.Errorf("expected modifiers [ctrl], got %v", p["modifiers"])
		}
	})
}

func TestE2E_Screenshot(t *testing.T) {
	srv, _ := setupE2ETest(t)
	ctx := context.Background()

	result, err := srv.handleScreenshot(ctx, toolReq(map[string]any{
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
	srv, _ := setupE2ETest(t)
	ctx := context.Background()

	result, err := srv.handleGetGameState(ctx, toolReq(nil))
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
	srv, stub := setupE2ETest(t)
	ctx := context.Background()

	if stub.testServerConn != nil {
		stub.testServerConn.Close()
	}
	stub.Close()

	result, err := srv.handleGetTree(ctx, toolReq(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result after disconnect")
	}
}

func TestE2E_WaitForNode(t *testing.T) {
	tests := []struct {
		name         string
		state        string
		wantContains string
	}{
		{"StateExists", "exists", `"found":true`},
		{"StateVisible", "visible", `"visible":true`},
		{"StateRemoved", "removed", `"removed":true`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, stub := setupE2ETest(t)
			ctx := context.Background()

			args := map[string]any{
				"selector":   "class:Button",
				"timeout_ms": float64(500),
				"state":      tt.state,
			}

			result, err := srv.handleWaitForNode(ctx, toolReq(args))
			if err != nil {
				t.Fatalf("handleWaitForNode: %v", err)
			}
			if result.IsError {
				t.Fatalf("wait_for_node error: %+v", result)
			}
			text := mustText(t, result)
			if !strings.Contains(text, tt.wantContains) {
				t.Errorf("wait_for_node result missing %q: %s", tt.wantContains, text)
			}

			// Verify state was forwarded in params.
			params := stub.lastCallParams("wait_for_node")
			if params == nil {
				t.Fatal("no wait_for_node params recorded")
			}
			var p map[string]any
			if err := json.Unmarshal(params, &p); err != nil {
				t.Fatalf("unmarshal wait_for_node params: %v", err)
			}
			if p["state"] != tt.state {
				t.Errorf("state = %v, want %q", p["state"], tt.state)
			}
		})
	}

	// Verify omitting state defaults to "exists".
	t.Run("DefaultState", func(t *testing.T) {
		srv, stub := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleWaitForNode(ctx, toolReq(map[string]any{
			"selector":   "class:Button",
			"timeout_ms": float64(500),
		}))
		if err != nil {
			t.Fatalf("handleWaitForNode: %v", err)
		}
		if result.IsError {
			t.Fatalf("wait_for_node error: %+v", result)
		}
		text := mustText(t, result)
		if !strings.Contains(text, `"found":true`) {
			t.Errorf("wait_for_node result missing %q: %s", `"found":true`, text)
		}

		params := stub.lastCallParams("wait_for_node")
		if params == nil {
			t.Fatal("no wait_for_node params recorded")
		}
		var p map[string]any
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatalf("unmarshal wait_for_node params: %v", err)
		}
		if p["state"] != "exists" {
			t.Errorf("default state = %v, want %q", p["state"], "exists")
		}
	})
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
