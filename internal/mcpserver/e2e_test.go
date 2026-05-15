package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
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
	case "get_performance":
		resp.Result = rawJSON(`{"metrics":{"TIME_FPS":60.0,"TIME_PROCESS":0.016,"TIME_PHYSICS_PROCESS":0.008,"MEMORY_STATIC":1048576,"OBJECT_COUNT":42,"RENDER_TOTAL_DRAW_CALLS_IN_FRAME":10}}`)
	case "assert_performance":
		p, _ := req.Params.(map[string]any)
		if p == nil {
			p = map[string]any{}
		}
		monitor, _ := p["monitor"].(string)
		threshold, _ := p["threshold"].(float64)
		op, _ := p["op"].(string)
		if op == "" {
			op = "lte"
		}
		// Stub: TIME_FPS = 60, everything else = 1024
		value := 60.0
		if monitor != "TIME_FPS" {
			value = 1024.0
		}
		passed := false
		switch op {
		case "lt":
			passed = value < threshold
		case "lte":
			passed = value <= threshold
		case "gt":
			passed = value > threshold
		case "gte":
			passed = value >= threshold
		case "eq":
			passed = value == threshold
		}
		assertResult := map[string]any{
			"passed":    passed,
			"monitor":   monitor,
			"value":     value,
			"threshold": threshold,
			"op":        op,
		}
		if !passed {
			assertResult["message"] = fmt.Sprintf("%s: %.2f does not satisfy %s %.2f", monitor, value, op, threshold)
		}
		b, _ := json.Marshal(assertResult)
		resp.Result = b
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

func TestE2E_GetPerformance(t *testing.T) {
	t.Run("DefaultMetrics", func(t *testing.T) {
		srv, _ := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleGetPerformance(ctx, toolReq(nil))
		if err != nil {
			t.Fatalf("handleGetPerformance: %v", err)
		}
		if result.IsError {
			t.Fatalf("get_performance error: %+v", result)
		}
		text := mustText(t, result)
		if !strings.Contains(text, "TIME_FPS") {
			t.Errorf("expected TIME_FPS in response, got: %s", text)
		}
	})

	t.Run("SpecificMonitor", func(t *testing.T) {
		srv, stub := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleGetPerformance(ctx, toolReq(map[string]any{
			"monitors": []any{"TIME_FPS", "MEMORY_STATIC"},
		}))
		if err != nil {
			t.Fatalf("handleGetPerformance with monitors: %v", err)
		}
		if result.IsError {
			t.Fatalf("get_performance error: %+v", result)
		}
		text := mustText(t, result)
		if !strings.Contains(text, "TIME_FPS") {
			t.Errorf("expected TIME_FPS in response, got: %s", text)
		}
		if n := stub.callCount("get_performance"); n != 1 {
			t.Errorf("expected 1 get_performance call, got %d", n)
		}
		params := stub.lastCallParams("get_performance")
		if params == nil {
			t.Fatal("no get_performance params recorded")
		}
		var p map[string]any
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatalf("unmarshal get_performance params: %v", err)
		}
		monitors, ok := p["monitors"].([]any)
		if !ok || len(monitors) != 2 {
			t.Errorf("expected monitors param with 2 entries, got %v", p["monitors"])
		}
	})
}

func TestE2E_AssertPerformance(t *testing.T) {
	t.Run("PassingAssertion", func(t *testing.T) {
		srv, _ := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleAssertPerformance(ctx, toolReq(map[string]any{
			"monitor":   "TIME_FPS",
			"threshold": float64(30),
			"op":        "gte",
		}))
		if err != nil {
			t.Fatalf("handleAssertPerformance: %v", err)
		}
		if result.IsError {
			t.Fatalf("assert_performance failed unexpectedly: %+v", result)
		}
		text := mustText(t, result)
		if !strings.Contains(text, `"passed":true`) {
			t.Errorf("expected passed=true, got: %s", text)
		}
	})

	t.Run("FailingAssertion", func(t *testing.T) {
		srv, _ := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleAssertPerformance(ctx, toolReq(map[string]any{
			"monitor":   "TIME_FPS",
			"threshold": float64(90),
			"op":        "gte",
		}))
		if err != nil {
			t.Fatalf("handleAssertPerformance: %v", err)
		}
		if !result.IsError {
			t.Fatal("expected error when assertion fails")
		}
		text := mustText(t, result)
		if !strings.Contains(text, `"passed":false`) {
			t.Errorf("expected passed=false, got: %s", text)
		}
	})

	t.Run("DefaultOpIsLte", func(t *testing.T) {
		srv, _ := setupE2ETest(t)
		ctx := context.Background()

		// TIME_FPS stub value is 60; 60 <= 60 should pass with default lte
		result, err := srv.handleAssertPerformance(ctx, toolReq(map[string]any{
			"monitor":   "TIME_FPS",
			"threshold": float64(60),
		}))
		if err != nil {
			t.Fatalf("handleAssertPerformance default op: %v", err)
		}
		if result.IsError {
			t.Fatalf("expected pass with default lte op: %+v", result)
		}
	})

	t.Run("MissingMonitor", func(t *testing.T) {
		srv, _ := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleAssertPerformance(ctx, toolReq(map[string]any{
			"threshold": float64(60),
		}))
		if err != nil {
			t.Fatalf("handleAssertPerformance missing monitor: %v", err)
		}
		if !result.IsError {
			t.Fatal("expected error when monitor is missing")
		}
	})

	t.Run("MissingThreshold", func(t *testing.T) {
		srv, _ := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleAssertPerformance(ctx, toolReq(map[string]any{
			"monitor": "TIME_FPS",
		}))
		if err != nil {
			t.Fatalf("handleAssertPerformance missing threshold: %v", err)
		}
		if !result.IsError {
			t.Fatal("expected error when threshold is missing")
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
