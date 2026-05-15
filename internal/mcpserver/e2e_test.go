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
	case "input_mouse", "input_action", "input_key", "input_touch":
		resp.Result = rawJSON(`{"success":true}`)
	case "screenshot":
		resp.Result = rawJSON(`{"data":"iVBORw0KGgo=","mime_type":"image/png","width":1280,"height":720}`)
	case "change_scene":
		resp.Result = rawJSON(`{"success":true,"previous_scene":"res://main.tscn","new_scene":"res://scenes/game.tscn"}`)
	case "wait_signal":
		// Simulate timeout for nonexistent_signal, success for others.
		params, _ := json.Marshal(req.Params)
		var p map[string]any
		json.Unmarshal(params, &p)
		if p["signal_name"] == "nonexistent_signal" {
			resp.Result = rawJSON(`{"received":false,"elapsed_ms":100,"reason":"timeout"}`)
		} else {
			resp.Result = rawJSON(`{"received":true,"elapsed_ms":42,"args":[]}`)
		}
	case "call_method":
		resp.Result = rawJSON(`{"result":42,"type":"int"}`)
	case "evaluate":
		resp.Result = rawJSON(`{"value":"hello","type":"String"}`)
	case "wait_for_property":
		resp.Result = rawJSON(`{"satisfied":true,"value":"Start Game","elapsed_ms":50}`)
	case "input_text":
		resp.Result = rawJSON(`{"success":true,"characters_sent":5}`)
	case "input_mouse_move":
		resp.Result = rawJSON(`{"success":true,"position":{"x":100,"y":200}}`)
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

func TestE2E_WaitForSignal(t *testing.T) {
	t.Run("SignalReceived", func(t *testing.T) {
		srv, stub := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleWaitForSignal(ctx, toolReq(map[string]any{
			"selector":    "/root/UI/StartButton",
			"signal_name": "pressed",
		}))
		if err != nil {
			t.Fatalf("handleWaitForSignal: %v", err)
		}
		if result.IsError {
			t.Fatalf("wait_for_signal error: %+v", result)
		}
		text := mustText(t, result)
		if !strings.Contains(text, "received") {
			t.Errorf("expected result to contain %q, got: %s", "received", text)
		}

		// Verify stub received the wait_signal call with correct params.
		if n := stub.callCount("wait_signal"); n != 1 {
			t.Errorf("expected 1 wait_signal call, got %d", n)
		}
		params := stub.lastCallParams("wait_signal")
		if params == nil {
			t.Fatal("no wait_signal params recorded")
		}
		var p map[string]any
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatalf("unmarshal wait_signal params: %v", err)
		}
		if p["selector"] != "/root/UI/StartButton" {
			t.Errorf("selector = %v, want /root/UI/StartButton", p["selector"])
		}
		if p["signal_name"] != "pressed" {
			t.Errorf("signal_name = %v, want pressed", p["signal_name"])
		}
	})

	t.Run("Timeout", func(t *testing.T) {
		srv, stub := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleWaitForSignal(ctx, toolReq(map[string]any{
			"selector":    "/root/UI/StartButton",
			"signal_name": "nonexistent_signal",
			"timeout_ms":  float64(100),
		}))
		if err != nil {
			t.Fatalf("handleWaitForSignal timeout: %v", err)
		}
		if result.IsError {
			t.Fatalf("wait_for_signal timeout error: %+v", result)
		}
		text := mustText(t, result)
		if !strings.Contains(text, "timeout") {
			t.Errorf("expected result to contain %q, got: %s", "timeout", text)
		}

		// Verify correct params were sent.
		params := stub.lastCallParams("wait_signal")
		if params == nil {
			t.Fatal("no wait_signal params recorded")
		}
		var p map[string]any
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatalf("unmarshal wait_signal params: %v", err)
		}
		if p["signal_name"] != "nonexistent_signal" {
			t.Errorf("signal_name = %v, want nonexistent_signal", p["signal_name"])
		}
		// Verify timeout_ms was passed through.
		if tm, ok := p["timeout_ms"].(float64); !ok || tm != 100 {
			t.Errorf("timeout_ms = %v, want 100", p["timeout_ms"])
		}
	})

	t.Run("MissingSelector", func(t *testing.T) {
		srv, _ := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleWaitForSignal(ctx, toolReq(map[string]any{
			"signal_name": "pressed",
		}))
		if err != nil {
			t.Fatalf("handleWaitForSignal missing selector: %v", err)
		}
		if !result.IsError {
			t.Fatal("expected error when selector is missing")
		}
	})

	t.Run("MissingSignalName", func(t *testing.T) {
		srv, _ := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleWaitForSignal(ctx, toolReq(map[string]any{
			"selector": "/root/UI/StartButton",
		}))
		if err != nil {
			t.Fatalf("handleWaitForSignal missing signal_name: %v", err)
		}
		if !result.IsError {
			t.Fatal("expected error when signal_name is missing")
		}
	})
}

func TestE2E_CallMethod(t *testing.T) {
	t.Run("BasicCall", func(t *testing.T) {
		srv, stub := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleCallMethod(ctx, toolReq(map[string]any{
			"selector": "class:Button",
			"method":   "get_text",
		}))
		if err != nil {
			t.Fatalf("handleCallMethod: %v", err)
		}
		if result.IsError {
			t.Fatalf("call_method error: %+v", result)
		}
		text := mustText(t, result)
		if !strings.Contains(text, "42") {
			t.Errorf("call_method result missing expected value: %s", text)
		}

		if n := stub.callCount("call_method"); n != 1 {
			t.Errorf("expected 1 call_method call, got %d", n)
		}
		params := stub.lastCallParams("call_method")
		if params == nil {
			t.Fatal("no call_method params recorded")
		}
		var p map[string]any
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatalf("unmarshal call_method params: %v", err)
		}
		if p["selector"] != "class:Button" {
			t.Errorf("call_method selector = %v, want class:Button", p["selector"])
		}
		if p["method"] != "get_text" {
			t.Errorf("call_method method = %v, want get_text", p["method"])
		}
	})

	t.Run("BlockedMethod", func(t *testing.T) {
		srv, _ := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleCallMethod(ctx, toolReq(map[string]any{
			"selector": "class:Button",
			"method":   "free",
		}))
		if err != nil {
			t.Fatalf("handleCallMethod blocked: %v", err)
		}
		if !result.IsError {
			t.Fatal("expected error for blocked method 'free'")
		}
	})

	t.Run("WithArgs", func(t *testing.T) {
		srv, stub := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleCallMethod(ctx, toolReq(map[string]any{
			"selector": "class:Button",
			"method":   "set_text",
			"args":     []any{"Hello"},
		}))
		if err != nil {
			t.Fatalf("handleCallMethod with args: %v", err)
		}
		if result.IsError {
			t.Fatalf("call_method with args error: %+v", result)
		}

		params := stub.lastCallParams("call_method")
		var p map[string]any
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatalf("unmarshal call_method params: %v", err)
		}
		if _, ok := p["args"]; !ok {
			t.Error("expected args field in call_method params")
		}
	})
}

func TestE2E_Evaluate(t *testing.T) {
	t.Run("BasicExpression", func(t *testing.T) {
		srv, stub := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleEvaluate(ctx, toolReq(map[string]any{
			"expression": `"hello"`,
		}))
		if err != nil {
			t.Fatalf("handleEvaluate: %v", err)
		}
		if result.IsError {
			t.Fatalf("evaluate error: %+v", result)
		}
		text := mustText(t, result)
		if !strings.Contains(text, "hello") {
			t.Errorf("evaluate result missing expected value: %s", text)
		}

		if n := stub.callCount("evaluate"); n != 1 {
			t.Errorf("expected 1 evaluate call, got %d", n)
		}
		params := stub.lastCallParams("evaluate")
		if params == nil {
			t.Fatal("no evaluate params recorded")
		}
		var p map[string]any
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatalf("unmarshal evaluate params: %v", err)
		}
		if p["expression"] != `"hello"` {
			t.Errorf("evaluate expression = %v, want %q", p["expression"], `"hello"`)
		}
	})

	t.Run("WithContextNode", func(t *testing.T) {
		srv, stub := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleEvaluate(ctx, toolReq(map[string]any{
			"expression":   "name",
			"context_node": "/root/UI/StartButton",
		}))
		if err != nil {
			t.Fatalf("handleEvaluate with context_node: %v", err)
		}
		if result.IsError {
			t.Fatalf("evaluate with context_node error: %+v", result)
		}

		params := stub.lastCallParams("evaluate")
		var p map[string]any
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatalf("unmarshal evaluate params: %v", err)
		}
		if p["context_node"] != "/root/UI/StartButton" {
			t.Errorf("evaluate context_node = %v, want /root/UI/StartButton", p["context_node"])
		}
	})
}

func TestE2E_WaitForProperty(t *testing.T) {
	srv, stub := setupE2ETest(t)
	ctx := context.Background()

	result, err := srv.handleWaitForProperty(ctx, toolReq(map[string]any{
		"selector":       "class:Button",
		"property":       "text",
		"operator":       "equals",
		"expected_value": "Start Game",
		"timeout_ms":     float64(5000),
	}))
	if err != nil {
		t.Fatalf("handleWaitForProperty: %v", err)
	}
	if result.IsError {
		t.Fatalf("wait_for_property error: %+v", result)
	}
	text := mustText(t, result)
	if !strings.Contains(text, "satisfied") {
		t.Errorf("wait_for_property result missing 'satisfied': %s", text)
	}

	if n := stub.callCount("wait_for_property"); n != 1 {
		t.Errorf("expected 1 wait_for_property call, got %d", n)
	}
	params := stub.lastCallParams("wait_for_property")
	if params == nil {
		t.Fatal("no wait_for_property params recorded")
	}
	var p map[string]any
	if err := json.Unmarshal(params, &p); err != nil {
		t.Fatalf("unmarshal wait_for_property params: %v", err)
	}
	if p["selector"] != "class:Button" {
		t.Errorf("wait_for_property selector = %v, want class:Button", p["selector"])
	}
	if p["operator"] != "equals" {
		t.Errorf("wait_for_property operator = %v, want equals", p["operator"])
	}
}

func TestE2E_TypeText(t *testing.T) {
	t.Run("BasicTypeText", func(t *testing.T) {
		srv, stub := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleTypeText(ctx, toolReq(map[string]any{
			"text": "hello",
		}))
		if err != nil {
			t.Fatalf("handleTypeText: %v", err)
		}
		if result.IsError {
			t.Fatalf("type_text error: %+v", result)
		}
		text := mustText(t, result)
		if !strings.Contains(text, "success") {
			t.Errorf("type_text result missing 'success': %s", text)
		}

		if n := stub.callCount("input_text"); n != 1 {
			t.Errorf("expected 1 input_text call, got %d", n)
		}
		params := stub.lastCallParams("input_text")
		if params == nil {
			t.Fatal("no input_text params recorded")
		}
		var p map[string]any
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatalf("unmarshal input_text params: %v", err)
		}
		if p["text"] != "hello" {
			t.Errorf("input_text text = %v, want hello", p["text"])
		}
	})

	t.Run("WithSelector", func(t *testing.T) {
		srv, stub := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleTypeText(ctx, toolReq(map[string]any{
			"text":     "world",
			"selector": "class:LineEdit",
		}))
		if err != nil {
			t.Fatalf("handleTypeText with selector: %v", err)
		}
		if result.IsError {
			t.Fatalf("type_text with selector error: %+v", result)
		}

		params := stub.lastCallParams("input_text")
		var p map[string]any
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatalf("unmarshal input_text params: %v", err)
		}
		if p["selector"] != "class:LineEdit" {
			t.Errorf("input_text selector = %v, want class:LineEdit", p["selector"])
		}
	})
}

func TestE2E_MouseMove(t *testing.T) {
	t.Run("MoveToCoordinates", func(t *testing.T) {
		srv, stub := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleMouseMove(ctx, toolReq(map[string]any{
			"coordinates": map[string]any{"x": float64(100), "y": float64(200)},
		}))
		if err != nil {
			t.Fatalf("handleMouseMove: %v", err)
		}
		if result.IsError {
			t.Fatalf("mouse_move error: %+v", result)
		}
		text := mustText(t, result)
		if !strings.Contains(text, "success") {
			t.Errorf("mouse_move result missing 'success': %s", text)
		}

		if n := stub.callCount("input_mouse_move"); n != 1 {
			t.Errorf("expected 1 input_mouse_move call, got %d", n)
		}
	})

	t.Run("MoveToSelector", func(t *testing.T) {
		srv, stub := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleMouseMove(ctx, toolReq(map[string]any{
			"selector": "class:Button",
		}))
		if err != nil {
			t.Fatalf("handleMouseMove by selector: %v", err)
		}
		if result.IsError {
			t.Fatalf("mouse_move by selector error: %+v", result)
		}

		if n := stub.callCount("input_mouse_move"); n != 1 {
			t.Errorf("expected 1 input_mouse_move call, got %d", n)
		}
		params := stub.lastCallParams("input_mouse_move")
		if params == nil {
			t.Fatal("no input_mouse_move params recorded")
		}
		var p map[string]any
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatalf("unmarshal input_mouse_move params: %v", err)
		}
		if p["selector"] != "class:Button" {
			t.Errorf("input_mouse_move selector = %v, want class:Button", p["selector"])
		}
	})

	t.Run("MissingTarget", func(t *testing.T) {
		srv, _ := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleMouseMove(ctx, toolReq(map[string]any{}))
		if err != nil {
			t.Fatalf("handleMouseMove missing target: %v", err)
		}
		if !result.IsError {
			t.Fatal("expected error when neither selector nor coordinates provided")
		}
	})
}

func TestE2E_Touch(t *testing.T) {
	t.Run("TapAtPosition", func(t *testing.T) {
		srv, stub := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleTouch(ctx, toolReq(map[string]any{
			"position": map[string]any{"x": float64(100), "y": float64(200)},
		}))
		if err != nil {
			t.Fatalf("handleTouch: %v", err)
		}
		if result.IsError {
			t.Fatalf("touch error: %+v", result)
		}
		text := mustText(t, result)
		if !strings.Contains(text, "success") {
			t.Errorf("touch result missing %q: %s", "success", text)
		}

		if n := stub.callCount("input_touch"); n != 1 {
			t.Errorf("expected 1 input_touch call, got %d", n)
		}
		params := stub.lastCallParams("input_touch")
		if params == nil {
			t.Fatal("no input_touch params recorded")
		}
		var p map[string]any
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatalf("unmarshal input_touch params: %v", err)
		}
		pos, ok := p["position"].(map[string]any)
		if !ok {
			t.Fatalf("expected position map, got %T", p["position"])
		}
		if pos["x"] != float64(100) || pos["y"] != float64(200) {
			t.Errorf("expected position {100, 200}, got %v", pos)
		}
	})

	t.Run("TouchWithDrag", func(t *testing.T) {
		srv, stub := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleTouch(ctx, toolReq(map[string]any{
			"position": map[string]any{"x": float64(50), "y": float64(100)},
			"drag_to":  map[string]any{"x": float64(200), "y": float64(300)},
			"index":    float64(0),
		}))
		if err != nil {
			t.Fatalf("handleTouch with drag: %v", err)
		}
		if result.IsError {
			t.Fatalf("touch drag error: %+v", result)
		}
		if n := stub.callCount("input_touch"); n != 1 {
			t.Errorf("expected 1 input_touch call, got %d", n)
		}
		params := stub.lastCallParams("input_touch")
		if params == nil {
			t.Fatal("no input_touch params recorded")
		}
		var p map[string]any
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatalf("unmarshal input_touch params: %v", err)
		}
		if p["drag_to"] == nil {
			t.Error("expected drag_to in params")
		}
	})

	t.Run("TouchMissingPosition", func(t *testing.T) {
		srv, _ := setupE2ETest(t)
		ctx := context.Background()

		result, err := srv.handleTouch(ctx, toolReq(map[string]any{}))
		if err != nil {
			t.Fatalf("handleTouch missing position: %v", err)
		}
		if !result.IsError {
			t.Fatal("expected error when position not provided")
		}
	})
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
