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
	case "input_mouse", "input_action", "input_key":
		resp.Result = rawJSON(`{"success":true}`)
	case "screenshot":
		resp.Result = rawJSON(`{"data":"iVBORw0KGgo=","mime_type":"image/png","width":1280,"height":720}`)
	case "change_scene":
		resp.Result = rawJSON(`{"success":true,"previous_scene":"res://main.tscn","new_scene":"res://scenes/game.tscn"}`)
	case "record_start":
		resp.Result = rawJSON(`{"success":true,"output_path":"res://recordings/run1.json"}`)
	case "record_stop":
		resp.Result = rawJSON(`{"success":true,"frames":42}`)
	case "replay":
		resp.Result = rawJSON(`{"success":true,"input_path":"res://recordings/run1.json"}`)
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

func TestE2E_ChangeScene(t *testing.T) {
	srv, stub := setupE2ETest(t)
	ctx := context.Background()

	result, err := srv.handleChangeScene(ctx, toolReq(map[string]any{
		"scene_path": "res://scenes/game.tscn",
	}))
	if err != nil {
		t.Fatalf("handleChangeScene: %v", err)
	}
	if result.IsError {
		t.Fatalf("change_scene error: %+v", result)
	}
	text := mustText(t, result)
	if !strings.Contains(text, "success") {
		t.Errorf("change_scene result missing %q: %s", "success", text)
	}

	if n := stub.callCount("change_scene"); n != 1 {
		t.Errorf("expected 1 change_scene call, got %d", n)
	}
	params := stub.lastCallParams("change_scene")
	if params == nil {
		t.Fatal("no change_scene params recorded")
	}
	var p map[string]any
	if err := json.Unmarshal(params, &p); err != nil {
		t.Fatalf("unmarshal change_scene params: %v", err)
	}
	if p["scene_path"] != "res://scenes/game.tscn" {
		t.Errorf("change_scene scene_path = %v, want res://scenes/game.tscn", p["scene_path"])
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

func TestE2E_RecordStart(t *testing.T) {
	srv, stub := setupE2ETest(t)
	ctx := context.Background()

	result, err := srv.handleRecordStart(ctx, toolReq(map[string]any{
		"output_path": "res://recordings/run1.json",
	}))
	if err != nil {
		t.Fatalf("handleRecordStart: %v", err)
	}
	if result.IsError {
		t.Fatalf("record_start error: %+v", result)
	}
	text := mustText(t, result)
	if !strings.Contains(text, "success") {
		t.Errorf("record_start result missing %q: %s", "success", text)
	}

	if n := stub.callCount("record_start"); n != 1 {
		t.Errorf("expected 1 record_start call, got %d", n)
	}
	params := stub.lastCallParams("record_start")
	var p map[string]any
	if err := json.Unmarshal(params, &p); err != nil {
		t.Fatalf("unmarshal record_start params: %v", err)
	}
	if p["output_path"] != "res://recordings/run1.json" {
		t.Errorf("record_start output_path = %v, want res://recordings/run1.json", p["output_path"])
	}
}

func TestE2E_RecordStop(t *testing.T) {
	srv, stub := setupE2ETest(t)
	ctx := context.Background()

	result, err := srv.handleRecordStop(ctx, toolReq(map[string]any{}))
	if err != nil {
		t.Fatalf("handleRecordStop: %v", err)
	}
	if result.IsError {
		t.Fatalf("record_stop error: %+v", result)
	}
	text := mustText(t, result)
	if !strings.Contains(text, "success") {
		t.Errorf("record_stop result missing %q: %s", "success", text)
	}

	if n := stub.callCount("record_stop"); n != 1 {
		t.Errorf("expected 1 record_stop call, got %d", n)
	}
}

func TestE2E_Replay(t *testing.T) {
	srv, stub := setupE2ETest(t)
	ctx := context.Background()

	result, err := srv.handleReplay(ctx, toolReq(map[string]any{
		"input_path": "res://recordings/run1.json",
	}))
	if err != nil {
		t.Fatalf("handleReplay: %v", err)
	}
	if result.IsError {
		t.Fatalf("replay error: %+v", result)
	}
	text := mustText(t, result)
	if !strings.Contains(text, "success") {
		t.Errorf("replay result missing %q: %s", "success", text)
	}

	if n := stub.callCount("replay"); n != 1 {
		t.Errorf("expected 1 replay call, got %d", n)
	}
	params := stub.lastCallParams("replay")
	var p map[string]any
	if err := json.Unmarshal(params, &p); err != nil {
		t.Fatalf("unmarshal replay params: %v", err)
	}
	if p["input_path"] != "res://recordings/run1.json" {
		t.Errorf("replay input_path = %v, want res://recordings/run1.json", p["input_path"])
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
