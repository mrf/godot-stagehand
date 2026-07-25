package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mrf/godot-stagehand/internal/godotconn"
)

const testPNG1x1Base64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4z8DwHwAFAAH/iZk9HQAAAABJRU5ErkJggg=="

var hostGuidanceTokens = []string{
	"127.0.0.1",
	"localhost",
	"WSL",
	"gateway",
	"STAGEHAND_BIND_ADDRESS=0.0.0.0",
	"STAGEHAND_ALLOW_REMOTE=1",
	"auth_token",
}

func assertContainsAll(t *testing.T, text string, wants []string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("expected text to contain %q, got: %s", want, text)
		}
	}
}

func TestNew_RegistersAllTools(t *testing.T) {
	s := New()
	tools := s.mcp.ListTools()

	expected := []string{
		"godot_connect",
		"godot_launch",
		"godot_status",
		"godot_list_instances",
		"godot_disconnect",
		"godot_get_game_state",
		"godot_get_tree",
		"godot_find_nodes",
		"godot_get_property",
		"godot_set_property",
		"godot_click",
		"godot_press_key",
		"godot_press_action",
		"godot_touch",
		"godot_type_text",
		"godot_mouse_move",
		"godot_screenshot",
		"godot_screenshot_save_baseline",
		"godot_screenshot_diff",
		"godot_wait_for_node",
		"godot_wait_for_property",
		"godot_wait_for_signal",
		"godot_change_scene",
		"godot_call_method",
		"godot_evaluate",
		"godot_get_performance",
		"godot_assert_performance",
		"godot_record_start",
		"godot_record_stop",
		"godot_replay",
	}

	for _, name := range expected {
		if _, ok := tools[name]; !ok {
			t.Errorf("tool %q not registered", name)
		}
	}

	if got, want := len(tools), len(expected); got != want {
		t.Errorf("expected %d tools, got %d", want, got)
	}
}

func TestToolsReturnErrorWhenNotConnected(t *testing.T) {
	s := New()
	ctx := context.Background()

	tests := []struct {
		name    string
		handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
		args    map[string]any
	}{
		{"godot_get_game_state", s.handleGetGameState, nil},
		{"godot_get_tree", s.handleGetTree, nil},
		{"godot_find_nodes", s.handleFindNodes, map[string]any{"selector": "class:Node"}},
		{"godot_get_property", s.handleGetProperty, map[string]any{"selector": "/root", "property": "name"}},
		{"godot_set_property", s.handleSetProperty, map[string]any{"selector": "/root", "property": "name", "value": "test"}},
		{"godot_click", s.handleClick, map[string]any{"selector": "/root/Button"}},
		{"godot_press_key", s.handlePressKey, map[string]any{"key": "Enter"}},
		{"godot_press_action", s.handlePressAction, map[string]any{"action": "ui_accept"}},
		{"godot_touch", s.handleTouch, map[string]any{"position": map[string]any{"x": float64(100), "y": float64(200)}}},
		{"godot_screenshot", s.handleScreenshot, nil},
		{"godot_wait_for_signal", s.handleWaitForSignal, map[string]any{"selector": "/root/Button", "signal_name": "pressed"}},
		{"godot_call_method", s.handleCallMethod, map[string]any{"selector": "/root", "method": "get_name"}},
		{"godot_evaluate", s.handleEvaluate, map[string]any{"expression": "1+1"}},
		{"godot_get_performance", s.handleGetPerformance, nil},
		{"godot_assert_performance", s.handleAssertPerformance, map[string]any{"monitor": "TIME_FPS", "threshold": float64(60)}},
		{"godot_record_start", s.handleRecordStart, map[string]any{"output_path": "res://recordings/run1.json"}},
		{"godot_record_stop", s.handleRecordStop, nil},
		{"godot_replay", s.handleReplay, map[string]any{"input_path": "res://recordings/run1.json"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			if tt.args != nil {
				req.Params.Arguments = tt.args
			}
			result, err := tt.handler(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.IsError {
				t.Fatal("expected isError=true when not connected")
			}
			text, ok := mcp.AsTextContent(result.Content[0])
			if !ok {
				t.Fatal("expected TextContent")
			}
			if text.Text != "Not connected. Call godot_connect or godot_launch first." {
				t.Errorf("unexpected error text: %s", text.Text)
			}
		})
	}
}

func TestInvalidSelectorReturnsError(t *testing.T) {
	s := New()
	ctx := context.Background()

	// Selectors that are invalid: empty value after a recognized prefix
	invalidSelector := "name:"

	tests := []struct {
		name    string
		handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
		args    map[string]any
	}{
		{"godot_find_nodes", s.handleFindNodes, map[string]any{"selector": invalidSelector}},
		{"godot_get_property", s.handleGetProperty, map[string]any{"selector": invalidSelector, "property": "position"}},
		{"godot_set_property", s.handleSetProperty, map[string]any{"selector": invalidSelector, "property": "position", "value": 0}},
		{"godot_click", s.handleClick, map[string]any{"selector": invalidSelector}},
		{"godot_type_text", s.handleTypeText, map[string]any{"text": "hello", "selector": invalidSelector}},
		{"godot_mouse_move", s.handleMouseMove, map[string]any{"selector": invalidSelector}},
		{"godot_wait_for_node", s.handleWaitForNode, map[string]any{"selector": invalidSelector}},
		{"godot_wait_for_signal", s.handleWaitForSignal, map[string]any{"selector": invalidSelector, "signal_name": "pressed"}},
		{"godot_wait_for_property", s.handleWaitForProperty, map[string]any{"selector": invalidSelector, "property": "x", "operator": "exists"}},
		{"godot_call_method", s.handleCallMethod, map[string]any{"selector": invalidSelector, "method": "get_name"}},
		{"godot_evaluate context_node", s.handleEvaluate, map[string]any{"expression": "1+1", "context_node": invalidSelector}},
		{"godot_screenshot", s.handleScreenshot, map[string]any{"selector": invalidSelector}},
		{"godot_screenshot_save_baseline", s.handleSaveBaseline, map[string]any{"name": "test", "selector": invalidSelector}},
		{"godot_screenshot_diff", s.handleScreenshotDiff, map[string]any{"name": "test", "selector": invalidSelector}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = tt.args
			result, err := tt.handler(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.IsError {
				t.Fatal("expected isError=true for invalid selector")
			}
			text, ok := mcp.AsTextContent(result.Content[0])
			if !ok {
				t.Fatal("expected TextContent")
			}
			if !strings.Contains(text.Text, "invalid selector") {
				t.Errorf("expected 'invalid selector' in error text, got: %s", text.Text)
			}
		})
	}
}

func TestValidSelectorPassesThrough(t *testing.T) {
	s := New()
	ctx := context.Background()

	// These should fail with "not connected", not a selector error
	validSelectors := []string{
		"/root/Main",
		"class:Button",
		"name:OkBtn",
		"group:enemies",
		"name:dialog >> text:Cancel",
	}

	for _, sel := range validSelectors {
		t.Run(sel, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = map[string]any{"selector": sel}
			result, err := s.handleFindNodes(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.IsError {
				t.Fatal("expected isError=true (not connected)")
			}
			text, ok := mcp.AsTextContent(result.Content[0])
			if !ok {
				t.Fatal("expected TextContent")
			}
			if strings.Contains(text.Text, "invalid selector") {
				t.Errorf("valid selector %q incorrectly rejected: %s", sel, text.Text)
			}
		})
	}
}

// TestConnectClearsConnOnPingFailure verifies that when a WebSocket connection
// succeeds but the ping RPC fails (e.g. server closes immediately), the stored
// connection is cleared so subsequent calls return "not connected" rather than
// errors from the broken connection.
func TestConnectClearsConnOnPingFailure(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		var authRequest godotconn.Request
		if err := ws.ReadJSON(&authRequest); err != nil {
			return
		}
		authResult, _ := json.Marshal(map[string]bool{"authenticated": true})
		if err := ws.WriteJSON(godotconn.Response{JSONRPC: "2.0", ID: authRequest.ID, Result: authResult}); err != nil {
			return
		}
		// Authentication succeeds, then the peer closes before answering ping.
		var pingRequest godotconn.Request
		if err := ws.ReadJSON(&pingRequest); err != nil {
			return
		}
	}))
	defer srv.Close()

	_, portStr, _ := strings.Cut(srv.Listener.Addr().String(), ":")
	port, _ := strconv.Atoi(portStr)

	s := New()
	ctx := context.Background()

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"host":       "127.0.0.1",
		"port":       float64(port),
		"auth_token": testMCPAuthToken,
	}
	result, err := s.handleConnect(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected isError=true when ping fails")
	}

	// The broken connection must not remain stored.
	if s.getConn() != nil {
		t.Fatal("expected conn to be nil after ping failure, but it is still stored")
	}
}

func TestSecurityControlsAreExposedByConnectionTools(t *testing.T) {
	authProperty, ok := connectTool.InputSchema.Properties["auth_token"]
	if !ok {
		t.Fatal("godot_connect schema must expose auth_token")
	}
	authSchema, ok := authProperty.(map[string]any)
	if !ok {
		t.Fatalf("godot_connect auth_token schema has type %T, want map[string]any", authProperty)
	}
	authDescription, _ := authSchema["description"].(string)
	if !strings.Contains(authDescription, "STAGEHAND_AUTH_TOKEN") {
		t.Fatalf("godot_connect auth_token description must document fixed tokens: %q", authDescription)
	}
	authRequired := false
	for _, name := range connectTool.InputSchema.Required {
		if name == "auth_token" {
			authRequired = true
			break
		}
	}
	if !authRequired {
		t.Fatal("godot_connect auth_token must be required")
	}

	property, ok := launchTool.InputSchema.Properties["allow_unsafe"]
	if !ok {
		t.Fatal("godot_launch schema must expose allow_unsafe")
	}
	propertySchema, ok := property.(map[string]any)
	if !ok {
		t.Fatalf("godot_launch allow_unsafe schema has type %T, want map[string]any", property)
	}
	if got := propertySchema["default"]; got != false {
		t.Fatalf("godot_launch allow_unsafe default = %v, want false", got)
	}
}

func TestConnectAuthenticatesBeforePing(t *testing.T) {
	const authToken = "mcp-connect-auth-token"
	methods := make(chan string, 2)
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		authenticated := false
		for {
			var req godotconn.Request
			if err := ws.ReadJSON(&req); err != nil {
				return
			}
			methods <- req.Method
			if req.Method == "authenticate" {
				params, _ := req.Params.(map[string]any)
				if params["token"] != authToken {
					_ = ws.WriteJSON(godotconn.Response{
						JSONRPC: "2.0",
						ID:      req.ID,
						Error:   &godotconn.RPCError{Code: godotconn.CodeAuthenticationFailed, Message: "authentication failed"},
					})
					continue
				}
				authenticated = true
				result, _ := json.Marshal(map[string]bool{"authenticated": true})
				_ = ws.WriteJSON(godotconn.Response{JSONRPC: "2.0", ID: req.ID, Result: result})
				continue
			}
			if !authenticated {
				_ = ws.WriteJSON(godotconn.Response{
					JSONRPC: "2.0",
					ID:      req.ID,
					Error:   &godotconn.RPCError{Code: godotconn.CodeAuthenticationRequired, Message: "authentication required"},
				})
				continue
			}
			result := json.RawMessage(currentHandshakeJSON(nil))
			_ = ws.WriteJSON(godotconn.Response{JSONRPC: "2.0", ID: req.ID, Result: result})
		}
	}))
	defer srv.Close()
	_, port := serverHostPort(t, srv)

	s := New()
	defer s.clearConn()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"host":       "127.0.0.1",
		"port":       float64(port),
		"auth_token": authToken,
	}
	result, err := s.handleConnect(context.Background(), req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if result.IsError {
		text, _ := mcp.AsTextContent(result.Content[0])
		t.Fatalf("connect returned tool error: %s", text.Text)
	}
	for index, want := range []string{"authenticate", "ping"} {
		select {
		case got := <-methods:
			if got != want {
				t.Fatalf("method %d = %q, want %q", index, got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for method %q", want)
		}
	}
}

func TestConnectRequiresAuthenticationToken(t *testing.T) {
	s := New()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"host": "127.0.0.1",
		"port": float64(1),
	}
	result, err := s.handleConnect(context.Background(), req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected missing auth_token to be rejected")
	}
	text, _ := mcp.AsTextContent(result.Content[0])
	if !strings.Contains(text.Text, "auth_token") {
		t.Fatalf("missing-token error must mention auth_token: %s", text.Text)
	}
}

func TestConnectReturnsErrorForUnreachableHost(t *testing.T) {
	s := New()
	ctx := context.Background()

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"host":       "localhost",
		"port":       float64(19999), // unlikely to have anything listening
		"auth_token": testMCPAuthToken,
	}
	result, err := s.handleConnect(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected isError=true for unreachable host")
	}
	text, ok := mcp.AsTextContent(result.Content[0])
	if !ok {
		t.Fatal("expected TextContent")
	}
	assertContainsAll(t, text.Text, hostGuidanceTokens)
}

func TestLaunchRejectsHeadlessWhenScreenshotsExpected(t *testing.T) {
	s := New()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"project_path":       "/tmp/nonexistent-stagehand-project",
		"godot_bin":          "/tmp/nonexistent-godot",
		"headless":           true,
		"expect_screenshots": true,
		"timeout_ms":         float64(1000),
	}

	result, err := s.handleLaunch(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected launch to reject headless screenshot workflow")
	}
	text, ok := mcp.AsTextContent(result.Content[0])
	if !ok {
		t.Fatal("expected TextContent")
	}
	for _, want := range []string{"headless=true", "expect_screenshots=true", "headless=false"} {
		if !strings.Contains(text.Text, want) {
			t.Fatalf("launch error should mention %q, got: %s", want, text.Text)
		}
	}
}

func TestLaunchWarningsAndGuidanceDocumentScreenshotHosts(t *testing.T) {
	if !strings.Contains(headlessScreenshotWarning, "godot_screenshot") {
		t.Fatalf("headless warning should mention screenshots, got: %s", headlessScreenshotWarning)
	}

	guidance := connectionGuidance()
	assertContainsAll(t, guidance, hostGuidanceTokens)
	assertContainsAll(t, hostSelectionDescription, hostGuidanceTokens)
}

func TestStatusWhenNotConnected(t *testing.T) {
	s := New()
	ctx := context.Background()

	result, err := s.handleStatus(ctx, mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected no error from godot_status")
	}
	text, ok := mcp.AsTextContent(result.Content[0])
	if !ok {
		t.Fatal("expected TextContent")
	}
	if !strings.Contains(text.Text, "not connected") && !strings.Contains(text.Text, "No active") {
		t.Errorf("expected 'not connected' in status output, got: %s", text.Text)
	}
}

func TestStatusWhenConnected(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	_, portStr, _ := strings.Cut(srv.Listener.Addr().String(), ":")
	port, _ := strconv.Atoi(portStr)

	s := New()
	conn, err := godotconn.Dial(context.Background(), "127.0.0.1", port)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	s.setConn(conn)
	defer s.clearConn()

	result, err := s.handleStatus(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected no error from godot_status")
	}
	text, ok := mcp.AsTextContent(result.Content[0])
	if !ok {
		t.Fatal("expected TextContent")
	}
	if !strings.Contains(text.Text, "Connected") {
		t.Errorf("expected 'Connected' in status output, got: %s", text.Text)
	}
	if !strings.Contains(text.Text, "127.0.0.1") {
		t.Errorf("expected address in status output, got: %s", text.Text)
	}
}

func TestStatusReportsGaveUpAfterReconnectExhausted(t *testing.T) {
	t.Setenv("STAGEHAND_MAX_RECONNECT_ATTEMPTS", "1")

	upgrader := websocket.Upgrader{}
	firstConn := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		firstConn <- ws
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	_, portStr, _ := strings.Cut(srv.Listener.Addr().String(), ":")
	port, _ := strconv.Atoi(portStr)

	s := New()
	conn, err := godotconn.Dial(context.Background(), "127.0.0.1", port)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	s.setConn(conn)
	defer s.clearConn()

	// Sever the peer for good, then stop accepting new connections, so the
	// bounded reconnect budget is exhausted rather than succeeding again.
	if err := (<-firstConn).Close(); err != nil {
		t.Fatalf("drop first connection: %v", err)
	}
	srv.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !conn.ReconnectExhausted() {
		time.Sleep(10 * time.Millisecond)
	}
	if !conn.ReconnectExhausted() {
		t.Fatal("connection never gave up on a permanently dead peer")
	}

	result, err := s.handleStatus(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text, ok := mcp.AsTextContent(result.Content[0])
	if !ok {
		t.Fatal("expected TextContent")
	}
	if !strings.Contains(text.Text, "Disconnected") {
		t.Errorf("expected 'Disconnected' in status output, got: %s", text.Text)
	}
	if !strings.Contains(text.Text, "gave up") {
		t.Errorf("expected a give-up note in status output, got: %s", text.Text)
	}
}

// TestScreenshotSelectorForcesFullPageFalse verifies that handleScreenshot sets
// full_page=false when a selector is provided, even if the caller omits full_page.
// This guards against the regression where the selector was silently ignored because
// the addon only crops when full_page=false AND selector is present.
func TestScreenshotSelectorForcesFullPageFalse(t *testing.T) {
	upgrader := websocket.Upgrader{}
	var capturedParams map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		_, msg, err := ws.ReadMessage()
		if err != nil {
			return
		}

		var req struct {
			ID     int64          `json:"id"`
			Params map[string]any `json:"params"`
		}
		_ = json.Unmarshal(msg, &req)
		capturedParams = req.Params

		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  map[string]any{"data": testPNG1x1Base64, "mime_type": "image/png", "width": 1, "height": 1},
		}
		b, _ := json.Marshal(resp)
		_ = ws.WriteMessage(websocket.TextMessage, b)
	}))
	defer srv.Close()

	_, portStr, _ := strings.Cut(srv.Listener.Addr().String(), ":")
	port, _ := strconv.Atoi(portStr)

	s := New()
	conn, err := godotconn.Dial(context.Background(), "127.0.0.1", port)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	s.setConn(conn)
	defer s.clearConn()

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"selector": "class:Button"}
	result, err := s.handleScreenshot(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		text, _ := mcp.AsTextContent(result.Content[0])
		t.Fatalf("unexpected tool error: %s", text.Text)
	}
	if capturedParams == nil {
		t.Fatal("no params captured — mock server did not receive a message")
	}
	if fullPage, ok := capturedParams["full_page"]; !ok || fullPage != false {
		t.Errorf("expected full_page=false when selector is present, got full_page=%v (ok=%v)", fullPage, ok)
	}
}

// newErrorGodotServer starts a fake WebSocket server that always responds with
// a JSON-RPC result containing {"error": errMsg}. It returns a connected Server
// and a cleanup function.
func newErrorGodotServer(t *testing.T, errMsg string) (*Server, func()) {
	t.Helper()
	rawResult, err := json.Marshal(map[string]string{"error": errMsg})
	if err != nil {
		t.Fatalf("marshal error result: %v", err)
	}
	return newRawResultGodotServer(t, string(rawResult))
}

// newRawResultGodotServer starts a fake WebSocket server that always responds
// with the supplied JSON-RPC result payload.
func newRawResultGodotServer(t *testing.T, rawResult string) (*Server, func()) {
	t.Helper()
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			var req map[string]json.RawMessage
			if err := ws.ReadJSON(&req); err != nil {
				return
			}
			id := req["id"]
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(id),
				"result":  json.RawMessage(rawResult),
			}
			if err := ws.WriteJSON(resp); err != nil {
				return
			}
		}
	}))

	_, portStr, _ := strings.Cut(srv.Listener.Addr().String(), ":")
	port, _ := strconv.Atoi(portStr)

	s := New()
	conn, err := godotconn.Dial(context.Background(), "127.0.0.1", port)
	if err != nil {
		srv.Close()
		t.Fatalf("dial: %v", err)
	}
	s.setConn(conn)

	return s, func() {
		s.clearConn()
		srv.Close()
	}
}

// TestAddonErrorsPropagateAsMCPErrors verifies that when the Godot addon returns
// {"error": "..."} in the result, all tool handlers surface it as IsError=true.
func TestAddonErrorsPropagateAsMCPErrors(t *testing.T) {
	const addonErr = "Node not found"
	s, cleanup := newErrorGodotServer(t, addonErr)
	defer cleanup()

	type handlerFunc func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
	tests := []struct {
		name    string
		handler handlerFunc
		args    map[string]any
	}{
		{"godot_get_game_state", s.handleGetGameState, nil},
		{"godot_screenshot", s.handleScreenshot, nil},
		{"godot_get_tree", s.handleGetTree, nil},
		{"godot_find_nodes", s.handleFindNodes, map[string]any{"selector": "class:Node"}},
		{"godot_get_property", s.handleGetProperty, map[string]any{"selector": "/root", "property": "name"}},
		{"godot_set_property", s.handleSetProperty, map[string]any{"selector": "/root", "property": "name", "value": "test"}},
		{"godot_click", s.handleClick, map[string]any{"selector": "/root/Button"}},
		{"godot_press_key", s.handlePressKey, map[string]any{"key": "Enter"}},
		{"godot_press_action", s.handlePressAction, map[string]any{"action": "ui_accept"}},
		{"godot_touch", s.handleTouch, map[string]any{"position": map[string]any{"x": float64(100), "y": float64(200)}}},
		{"godot_type_text", s.handleTypeText, map[string]any{"text": "hello"}},
		{"godot_mouse_move", s.handleMouseMove, map[string]any{"coordinates": map[string]any{"x": float64(100), "y": float64(200)}}},
		{"godot_change_scene", s.handleChangeScene, map[string]any{"scene_path": "res://main.tscn"}},
		{"godot_call_method", s.handleCallMethod, map[string]any{"selector": "/root", "method": "get_name"}},
		{"godot_evaluate", s.handleEvaluate, map[string]any{"expression": "1+1"}},
		{"godot_wait_for_node", s.handleWaitForNode, map[string]any{"selector": "/root/Button"}},
		{"godot_wait_for_signal", s.handleWaitForSignal, map[string]any{"selector": "/root/Button", "signal_name": "pressed"}},
		{"godot_wait_for_property", s.handleWaitForProperty, map[string]any{"selector": "/root", "property": "name", "operator": "exists"}},
		{"godot_get_performance", s.handleGetPerformance, nil},
		{"godot_assert_performance", s.handleAssertPerformance, map[string]any{"monitor": "TIME_FPS", "threshold": float64(60)}},
		{"godot_record_start", s.handleRecordStart, map[string]any{"output_path": "res://rec.json"}},
		{"godot_record_stop", s.handleRecordStop, nil},
		{"godot_replay", s.handleReplay, map[string]any{"input_path": "res://rec.json"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			if tt.args != nil {
				req.Params.Arguments = tt.args
			}
			result, err := tt.handler(context.Background(), req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.IsError {
				t.Fatalf("expected IsError=true, got false; content: %v", result.Content)
			}
			text, ok := mcp.AsTextContent(result.Content[0])
			if !ok {
				t.Fatal("expected TextContent")
			}
			if !strings.Contains(text.Text, addonErr) {
				t.Errorf("expected error message %q in result, got: %s", addonErr, text.Text)
			}
		})
	}
}

func TestScreenshotRejectsInvalidPNGData(t *testing.T) {
	rawResult := `{"data":"bm90IGEgcG5n","mime_type":"image/png","width":1280,"height":720}`
	s, cleanup := newRawResultGodotServer(t, rawResult)
	defer cleanup()

	result, err := s.handleScreenshot(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected invalid screenshot data to return a tool error")
	}
	text, ok := mcp.AsTextContent(result.Content[0])
	if !ok {
		t.Fatal("expected TextContent error")
	}
	if !strings.Contains(text.Text, "failed to decode screenshot PNG") {
		t.Fatalf("expected PNG decode diagnostic, got: %s", text.Text)
	}
}

func TestScreenshotAddonErrorIncludesCodeAndDetails(t *testing.T) {
	rawResult := `{"error":"PNG encode produced zero bytes","error_code":"png_encode_empty","details":{"width":1280,"height":720,"next_action":"Run Godot with a visible window"}}`
	s, cleanup := newRawResultGodotServer(t, rawResult)
	defer cleanup()

	result, err := s.handleScreenshot(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected addon screenshot error to return a tool error")
	}
	text, ok := mcp.AsTextContent(result.Content[0])
	if !ok {
		t.Fatal("expected TextContent error")
	}
	for _, want := range []string{"PNG encode produced zero bytes", "png_encode_empty", "width", "1280", "next_action"} {
		if !strings.Contains(text.Text, want) {
			t.Fatalf("expected screenshot diagnostic to contain %q, got: %s", want, text.Text)
		}
	}
}

// TestClearConnNoRace verifies that concurrent calls to clearConn and getConn
// do not trigger the race detector. This guards against the pattern where
// handleConnect previously called getConn() then Close() without holding any
// lock, creating a window where another goroutine could observe a closing conn.
func TestClearConnNoRace(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	_, portStr, _ := strings.Cut(srv.Listener.Addr().String(), ":")
	port, _ := strconv.Atoi(portStr)

	s := New()
	conn, err := godotconn.Dial(context.Background(), "127.0.0.1", port)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	s.setConn(conn)

	var wg sync.WaitGroup
	const workers = 20
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				s.clearConn()
			} else {
				_ = s.getConn()
			}
		}()
	}
	wg.Wait()
}
