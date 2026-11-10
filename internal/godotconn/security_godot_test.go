//go:build godot

package godotconn

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const (
	testCodeAuthenticationRequired = -32001
	testCodeAuthenticationFailed   = -32002
	testCodeUnsafeCapability       = -32003
	generatedAuthTokenPrefix       = "Stagehand: Authentication token: "
)

func TestStagehandAuthenticationBoundary(t *testing.T) {
	conn, _, _ := startSecurityGodot(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	unauthenticatedCalls := []struct {
		method string
		params any
	}{
		{"ping", nil},
		{"get_tree", nil},
		{"set_property", map[string]any{
			"selector": "/root/TestScene/PropertyTarget",
			"property": "flag_prop",
			"value":    false,
		}},
		{"input_action", map[string]any{"action": "ui_accept", "pressed": true}},
		{"call_method", map[string]any{
			"selector": "/root/TestScene/PropertyTarget",
			"method":   "get_instance_id",
		}},
		{"evaluate", map[string]any{"expression": "1 + 1"}},
		{"change_scene", map[string]any{"scene_path": "res://main.tscn"}},
	}
	for _, call := range unauthenticatedCalls {
		t.Run("unauthenticated_"+call.method, func(t *testing.T) {
			_, err := conn.Call(ctx, call.method, call.params)
			requireSecurityRPCCode(t, err, testCodeAuthenticationRequired)
		})
	}

	authenticateSecurityConnection(t, ctx, conn)

	setResponse, err := conn.Call(ctx, "set_property", map[string]any{
		"selector": "/root/TestScene/PropertyTarget",
		"property": "flag_prop",
		"value":    false,
	})
	if err != nil {
		t.Fatalf("authenticated mutation failed: %v", err)
	}
	var setResult struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(setResponse.Result, &setResult); err != nil {
		t.Fatalf("decode set_property response: %v", err)
	}
	if !setResult.Success {
		t.Fatalf("authenticated mutation returned success=false: %s", setResponse.Result)
	}

	_, err = conn.Call(ctx, "evaluate", map[string]any{"expression": "1 + 1"})
	requireSecurityRPCCode(t, err, testCodeUnsafeCapability)
	requireSecurityRPCMessageContains(t, err, "STAGEHAND_ALLOW_UNSAFE=1")
	_, err = conn.Call(ctx, "call_method", map[string]any{
		"selector": "/root/TestScene/PropertyTarget",
		"method":   "get_instance_id",
	})
	requireSecurityRPCCode(t, err, testCodeUnsafeCapability)
	requireSecurityRPCMessageContains(t, err, "STAGEHAND_ALLOW_UNSAFE=1")

	secondConn, err := Dial(ctx, "127.0.0.1", securityConnectionPort(conn))
	if err != nil {
		t.Fatalf("connect second peer: %v", err)
	}
	defer secondConn.Close()
	_, err = secondConn.Call(ctx, "set_property", map[string]any{
		"selector": "/root/TestScene/PropertyTarget",
		"property": "flag_prop",
		"value":    true,
	})
	requireSecurityRPCCode(t, err, testCodeAuthenticationRequired)

	getResponse, err := conn.Call(ctx, "get_property", map[string]any{
		"selector": "/root/TestScene/PropertyTarget",
		"property": "flag_prop",
	})
	if err != nil {
		t.Fatalf("authenticated read-back failed: %v", err)
	}
	var getResult struct {
		Value bool `json:"value"`
	}
	if err := json.Unmarshal(getResponse.Result, &getResult); err != nil {
		t.Fatalf("decode get_property response: %v", err)
	}
	if getResult.Value {
		t.Fatal("unauthenticated second peer changed flag_prop")
	}
}

// One wrong token per connection: the server answers the failed authenticate
// and then closes the socket, so a short token can't be brute-forced over a
// single peer (godot-stagehand-ext-review-2026-11-327t.5). A raw socket is used
// because Connection would transparently redial after the close.
func TestStagehandFailedAuthenticationClosesPeer(t *testing.T) {
	_, port, _ := startSecurityGodot(t, nil)
	ws := dialRawSecurityWebSocket(t, port)

	request := `{"jsonrpc":"2.0","id":1,"method":"authenticate","params":{"token":"wrong-token"}}`
	if err := ws.WriteMessage(websocket.TextMessage, []byte(request)); err != nil {
		t.Fatalf("send authenticate: %v", err)
	}
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, payload, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read authenticate response: %v", err)
	}
	var response struct {
		Error *RPCError `json:"error"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("decode authenticate response %q: %v", payload, err)
	}
	if response.Error == nil || response.Error.Code != testCodeAuthenticationFailed {
		t.Fatalf("authenticate response = %s, want error code %d", payload, testCodeAuthenticationFailed)
	}
	requireSecurityPeerClosed(t, ws, 3*time.Second)
}

// A peer that upgrades but never authenticates is closed after the addon's
// AUTH_DEADLINE_MS (5s), so idle sockets can't hold the 32 client slots or keep
// quit-on-disconnect from firing (godot-stagehand-ext-review-2026-11-327t.5).
func TestStagehandUnauthenticatedPeerIsClosedAfterDeadline(t *testing.T) {
	_, port, _ := startSecurityGodot(t, nil)
	ws := dialRawSecurityWebSocket(t, port)
	opened := time.Now()

	requireSecurityPeerClosed(t, ws, 15*time.Second)
	if elapsed := time.Since(opened); elapsed < 4*time.Second {
		t.Fatalf("unauthenticated peer closed after %s, want ~5s (the auth deadline)", elapsed)
	}
}

func dialRawSecurityWebSocket(t *testing.T, port int) *websocket.Conn {
	t.Helper()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	ws, _, err := websocket.DefaultDialer.Dial("ws://"+address, nil)
	if err != nil {
		t.Fatalf("dial raw WebSocket: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

// requireSecurityPeerClosed reads until the server closes the socket, failing
// if it is still open after within.
func requireSecurityPeerClosed(t *testing.T, ws *websocket.Conn, within time.Duration) {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(within))
	for {
		_, payload, err := ws.ReadMessage()
		if err == nil {
			t.Logf("message before close: %s", payload)
			continue
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			t.Fatalf("peer still open %s later", within)
		}
		if !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
			t.Fatalf("peer closed with %v, want close code %d", err, websocket.ClosePolicyViolation)
		}
		return
	}
}

// Safe mode must not be one set_property away from unsafe mode: the server's
// own node and private (`_`-prefixed) members are off limits to the property
// surface (godot-stagehand-ext-review-2026-11-327t.1).
func TestStagehandSafeModeCannotBeDisabledRemotely(t *testing.T) {
	conn, _, _ := startSecurityGodot(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	authenticateSecurityConnection(t, ctx, conn)

	_, err := conn.Call(ctx, "set_property", map[string]any{
		"selector": "/root/StagehandServer",
		"property": "_allow_unsafe",
		"value":    true,
	})
	if err == nil {
		t.Error("set_property _allow_unsafe on /root/StagehandServer succeeded; want it refused")
	}
	_, err = conn.Call(ctx, "get_property", map[string]any{
		"selector": "/root/StagehandServer",
		"property": "_auth_token",
	})
	if err == nil {
		t.Error("get_property _auth_token on /root/StagehandServer succeeded; want it refused")
	}

	_, err = conn.Call(ctx, "evaluate", map[string]any{"expression": "1 + 1"})
	requireSecurityRPCCode(t, err, testCodeUnsafeCapability)
}

func TestStagehandUnsafeMethodsCanBeEnabledExplicitly(t *testing.T) {
	conn, _, _ := startSecurityGodot(t, []string{"STAGEHAND_ALLOW_UNSAFE=1"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	authenticateSecurityConnection(t, ctx, conn)

	response, err := conn.Call(ctx, "evaluate", map[string]any{"expression": "1 + 1"})
	if err != nil {
		t.Fatalf("evaluate with explicit unsafe opt-in: %v", err)
	}
	var result struct {
		Value float64 `json:"value"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatalf("decode evaluate response: %v", err)
	}
	if result.Value != 2 {
		t.Fatalf("evaluate result = %v, want 2", result.Value)
	}
	if _, err := conn.Call(ctx, "call_method", map[string]any{
		"selector": "/root/TestScene/PropertyTarget",
		"method":   "get_instance_id",
	}); err != nil {
		t.Fatalf("call_method with explicit unsafe opt-in: %v", err)
	}
}

func TestStagehandGeneratesSessionAuthenticationToken(t *testing.T) {
	conn, _, logPath := startSecurityGodotWithEnvironment(t, []string{"STAGEHAND_AUTH_TOKEN="})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log := readFileBestEffort(logPath)
	token := ""
	for _, line := range strings.Split(log, "\n") {
		if strings.HasPrefix(line, generatedAuthTokenPrefix) {
			token = strings.TrimSpace(strings.TrimPrefix(line, generatedAuthTokenPrefix))
			break
		}
	}
	if len(token) != 64 {
		t.Fatalf("generated authentication token length = %d, want 64 hex characters; log:\n%s", len(token), log)
	}
	if _, err := conn.Call(ctx, "authenticate", map[string]any{"token": token}); err != nil {
		t.Fatalf("authenticate with generated session token: %v", err)
	}
	if _, err := conn.Call(ctx, "get_tree", nil); err != nil {
		t.Fatalf("authenticated call with generated session token: %v", err)
	}
}

func TestStagehandBindPolicy(t *testing.T) {
	remoteIP := nonLoopbackIPv4(t)

	t.Run("default_is_loopback_only", func(t *testing.T) {
		_, port, _ := startSecurityGodot(t, nil)
		requireRemoteReachability(t, remoteIP, port, false)
	})

	t.Run("remote_bind_without_opt_in_is_rejected", func(t *testing.T) {
		_, port, _ := startSecurityGodot(t, []string{"STAGEHAND_BIND_ADDRESS=0.0.0.0"})
		requireRemoteReachability(t, remoteIP, port, false)
	})

	t.Run("explicit_remote_bind_warns", func(t *testing.T) {
		_, port, logPath := startSecurityGodot(t, []string{
			"STAGEHAND_BIND_ADDRESS=0.0.0.0",
			"STAGEHAND_ALLOW_REMOTE=1",
		})
		requireRemoteReachability(t, remoteIP, port, true)
		log := readFileBestEffort(logPath)
		if !strings.Contains(log, "WARNING") || !strings.Contains(log, "non-loopback") {
			t.Fatalf("remote bind log must contain a prominent non-loopback warning:\n%s", log)
		}
	})
}

func TestStagehandWebSocketKeepalive(t *testing.T) {
	_, port, _ := startSecurityGodot(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	liveness := livenessConfig{
		pingInterval: 25 * time.Millisecond,
		pongWait:     250 * time.Millisecond,
		writeTimeout: 100 * time.Millisecond,
	}
	conn, err := dialWithLiveness(ctx, "127.0.0.1", port, liveness)
	if err != nil {
		t.Fatalf("dial keepalive connection: %v", err)
	}
	defer conn.Close()
	if err := conn.Authenticate(ctx, testAuthToken); err != nil {
		t.Fatalf("authenticate keepalive connection: %v", err)
	}

	time.Sleep(3 * liveness.pongWait)
	if conn.State() != Connected {
		t.Fatalf("Godot connection state after keepalive window = %s, want Connected", conn.State())
	}
	if _, err := conn.Call(ctx, "ping", nil); err != nil {
		t.Fatalf("ping after WebSocket keepalive window: %v", err)
	}
}

func startSecurityGodot(t *testing.T, extraEnvironment []string) (*Connection, int, string) {
	t.Helper()
	environment := append([]string{"STAGEHAND_AUTH_TOKEN=" + testAuthToken}, extraEnvironment...)
	return startSecurityGodotWithEnvironment(t, environment)
}

func startSecurityGodotWithEnvironment(t *testing.T, environment []string) (*Connection, int, string) {
	t.Helper()
	godotBin, err := findGodotBinary()
	if err != nil {
		t.Fatal(err)
	}
	if godotBin == "" {
		t.Fatal("Godot binary not found; the godot build tag requires a Godot-equipped environment")
	}

	root := findProjectRoot(t)
	projectDir := prepareGodotTestProject(t, root)
	port := freeTCPPort(t)
	logPath := filepath.Join(t.TempDir(), "godot-security.log")
	cmd, wait := launchGodotWithEnvironment(t, godotBin, projectDir, port, logPath, environment)
	t.Cleanup(func() { stopProcess(cmd, wait) })

	ctx, cancel := context.WithTimeout(context.Background(), godotStartupTimeout)
	defer cancel()
	conn := dialUnauthenticatedGodotWhenReady(t, ctx, port, wait, logPath)
	t.Cleanup(func() { _ = conn.Close() })
	return conn, port, logPath
}

func authenticateSecurityConnection(t *testing.T, ctx context.Context, conn *Connection) {
	t.Helper()
	response, err := conn.Call(ctx, "authenticate", map[string]any{"token": testAuthToken})
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	var result struct {
		Authenticated bool `json:"authenticated"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatalf("decode authenticate response: %v", err)
	}
	if !result.Authenticated {
		t.Fatalf("authenticate returned false: %s", response.Result)
	}
}

func requireSecurityRPCCode(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected JSON-RPC error code %d, got nil", want)
	}
	var rpcError *RPCError
	if !errors.As(err, &rpcError) {
		t.Fatalf("expected RPCError code %d, got %T: %v", want, err, err)
	}
	if rpcError.Code != want {
		t.Fatalf("RPC error code = %d, want %d (message: %s)", rpcError.Code, want, rpcError.Message)
	}
}

func requireSecurityRPCMessageContains(t *testing.T, err error, want string) {
	t.Helper()
	var rpcError *RPCError
	if !errors.As(err, &rpcError) {
		t.Fatalf("expected RPCError, got %T: %v", err, err)
	}
	if !strings.Contains(rpcError.Message, want) {
		t.Fatalf("RPC error message = %q, want substring %q", rpcError.Message, want)
	}
}

func securityConnectionPort(conn *Connection) int {
	_, portText, err := net.SplitHostPort(conn.Addr())
	if err != nil {
		return 0
	}
	port, _ := strconv.Atoi(portText)
	return port
}

func nonLoopbackIPv4(t *testing.T) string {
	t.Helper()
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatalf("list network addresses: %v", err)
	}
	for _, address := range addresses {
		var ip net.IP
		switch value := address.(type) {
		case *net.IPNet:
			ip = value.IP
		case *net.IPAddr:
			ip = value.IP
		}
		if ipv4 := ip.To4(); ipv4 != nil && !ipv4.IsLoopback() {
			return ipv4.String()
		}
	}
	t.Fatal("no non-loopback IPv4 address available for bind-policy test")
	return ""
}

func requireRemoteReachability(t *testing.T, host string, port int, want bool) {
	t.Helper()
	address := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", address, 500*time.Millisecond)
	if conn != nil {
		_ = conn.Close()
	}
	if got := err == nil; got != want {
		t.Fatalf("remote reachability for %s = %t, want %t (error: %v)", address, got, want, err)
	}
}
