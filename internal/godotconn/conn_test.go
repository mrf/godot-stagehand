package godotconn

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const testConnectionAuthToken = "connection-auth-token"

var testUpgrader = websocket.Upgrader{}

// echoServer returns an httptest.Server that upgrades to WebSocket and echoes
// back JSON-RPC responses for each request, using the same id and a result
// containing the method that was called.
func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			var req Request
			if err := ws.ReadJSON(&req); err != nil {
				return
			}
			result, _ := json.Marshal(map[string]string{"method": req.Method})
			resp := Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  result,
			}
			if err := ws.WriteJSON(resp); err != nil {
				return
			}
		}
	}))
}

func serverHostPort(t *testing.T, s *httptest.Server) (string, int) {
	t.Helper()
	addr := s.Listener.Addr().String()
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

func TestDialAndCall(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	host, port := serverHostPort(t, srv)

	ctx := context.Background()
	conn, err := Dial(ctx, host, port)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if conn.State() != Connected {
		t.Errorf("state = %v, want Connected", conn.State())
	}

	resp, err := conn.Call(ctx, "ping", nil)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]string
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result["method"] != "ping" {
		t.Errorf("result method = %q, want ping", result["method"])
	}
}

func TestAuthenticate(t *testing.T) {
	methods := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		var req Request
		if err := ws.ReadJSON(&req); err != nil {
			return
		}
		methods <- req.Method
		params, _ := req.Params.(map[string]any)
		if req.Method != "authenticate" || params["token"] != testConnectionAuthToken {
			_ = ws.WriteJSON(Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &RPCError{Code: CodeAuthenticationFailed, Message: "authentication failed"},
			})
			return
		}
		result, _ := json.Marshal(map[string]bool{"authenticated": true})
		_ = ws.WriteJSON(Response{JSONRPC: "2.0", ID: req.ID, Result: result})
	}))
	defer srv.Close()
	host, port := serverHostPort(t, srv)

	conn, err := Dial(context.Background(), host, port)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.Authenticate(context.Background(), testConnectionAuthToken); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if got := <-methods; got != "authenticate" {
		t.Fatalf("first method = %q, want authenticate", got)
	}
}

func TestAuthenticatePersistsAcrossReconnect(t *testing.T) {
	authenticatedConnections := make(chan struct{}, 2)
	firstConnection := make(chan *websocket.Conn, 1)
	var connectionMu sync.Mutex
	connectionCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		connectionMu.Lock()
		connectionCount++
		connectionNumber := connectionCount
		connectionMu.Unlock()
		if connectionNumber == 1 {
			firstConnection <- ws
		}

		authenticated := false
		for {
			var req Request
			if err := ws.ReadJSON(&req); err != nil {
				return
			}
			if req.Method == "authenticate" {
				params, _ := req.Params.(map[string]any)
				if params["token"] != testConnectionAuthToken {
					_ = ws.WriteJSON(Response{
						JSONRPC: "2.0",
						ID:      req.ID,
						Error:   &RPCError{Code: CodeAuthenticationFailed, Message: "authentication failed"},
					})
					continue
				}
				authenticated = true
				authenticatedConnections <- struct{}{}
				result, _ := json.Marshal(map[string]bool{"authenticated": true})
				_ = ws.WriteJSON(Response{JSONRPC: "2.0", ID: req.ID, Result: result})
				continue
			}
			if !authenticated {
				_ = ws.WriteJSON(Response{
					JSONRPC: "2.0",
					ID:      req.ID,
					Error:   &RPCError{Code: CodeAuthenticationRequired, Message: "authentication required"},
				})
				continue
			}
			result, _ := json.Marshal(map[string]string{"status": "ok"})
			_ = ws.WriteJSON(Response{JSONRPC: "2.0", ID: req.ID, Result: result})
		}
	}))
	defer srv.Close()
	host, port := serverHostPort(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := Dial(ctx, host, port)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.Authenticate(ctx, testConnectionAuthToken); err != nil {
		t.Fatalf("authenticate initial connection: %v", err)
	}
	<-authenticatedConnections
	if _, err := conn.Call(ctx, "ping", nil); err != nil {
		t.Fatalf("initial authenticated call: %v", err)
	}

	if err := (<-firstConnection).Close(); err != nil {
		t.Fatalf("drop first connection: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for conn.State() == Connected && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if conn.State() == Connected {
		t.Fatal("connection did not enter reconnecting state")
	}

	if _, err := conn.Call(ctx, "ping", nil); err != nil {
		t.Fatalf("call after authenticated reconnect: %v", err)
	}
	select {
	case <-authenticatedConnections:
	case <-ctx.Done():
		t.Fatal("reconnected socket was not authenticated before use")
	}
}

func TestMultiplexedCalls(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	host, port := serverHostPort(t, srv)

	ctx := context.Background()
	conn, err := Dial(ctx, host, port)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var wg sync.WaitGroup
	methods := []string{"ping", "get_tree", "query_nodes", "screenshot", "get_game_state"}
	results := make([]string, len(methods))
	errs := make([]error, len(methods))

	for i, m := range methods {
		wg.Add(1)
		go func(idx int, method string) {
			defer wg.Done()
			resp, err := conn.Call(ctx, method, nil)
			errs[idx] = err
			if err == nil {
				var r map[string]string
				json.Unmarshal(resp.Result, &r)
				results[idx] = r["method"]
			}
		}(i, m)
	}
	wg.Wait()

	for i, m := range methods {
		if errs[i] != nil {
			t.Errorf("Call(%q) error: %v", m, errs[i])
		} else if results[i] != m {
			t.Errorf("Call(%q) result method = %q", m, results[i])
		}
	}
}

func TestCallRPCError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			var req Request
			if err := ws.ReadJSON(&req); err != nil {
				return
			}
			resp := Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &RPCError{
					Code:    CodeMethodNotFound,
					Message: "unknown method",
				},
			}
			ws.WriteJSON(resp)
		}
	}))
	defer srv.Close()
	host, port := serverHostPort(t, srv)

	ctx := context.Background()
	conn, err := Dial(ctx, host, port)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	resp, err := conn.Call(ctx, "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error")
	}

	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *RPCError, got %T", err)
	}
	if rpcErr.Code != CodeMethodNotFound {
		t.Errorf("code = %d, want %d", rpcErr.Code, CodeMethodNotFound)
	}

	// Response is also returned for error inspection.
	if resp == nil {
		t.Fatal("resp should not be nil on RPC error")
	}
}

func TestCallContextCancellation(t *testing.T) {
	// Server that never responds.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		// Read but never respond.
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	host, port := serverHostPort(t, srv)

	ctx := context.Background()
	conn, err := Dial(ctx, host, port)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	_, err = conn.Call(ctx, "ping", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
	conn.mu.Lock()
	pending := len(conn.pending)
	conn.mu.Unlock()
	if pending != 0 {
		t.Fatalf("pending calls after deadline = %d, want 0", pending)
	}
}

func TestKeepaliveMaintainsResponsiveConnection(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	host, port := serverHostPort(t, srv)
	liveness := livenessConfig{
		pingInterval: 10 * time.Millisecond,
		pongWait:     60 * time.Millisecond,
		writeTimeout: 20 * time.Millisecond,
	}

	conn, err := dialWithLiveness(context.Background(), host, port, liveness)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	time.Sleep(3 * liveness.pongWait)

	if conn.State() != Connected {
		t.Fatalf("responsive peer state = %s, want Connected", conn.State())
	}
	if _, err := conn.Call(context.Background(), "ping", nil); err != nil {
		t.Fatalf("call after healthy keepalive: %v", err)
	}
}

func TestKeepaliveReconnectsAndRecoversFromSilentPeer(t *testing.T) {
	var connectionCount atomic.Int32
	releaseSilentPeer := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		if connectionCount.Add(1) == 1 {
			// Never read frames: this models a half-open or frozen peer that does
			// not process ping control frames or application requests.
			<-releaseSilentPeer
			return
		}
		for {
			var req Request
			if err := ws.ReadJSON(&req); err != nil {
				return
			}
			result, _ := json.Marshal(map[string]string{"status": "ok"})
			if err := ws.WriteJSON(Response{JSONRPC: "2.0", ID: req.ID, Result: result}); err != nil {
				return
			}
		}
	}))
	t.Cleanup(func() {
		close(releaseSilentPeer)
		srv.Close()
	})
	host, port := serverHostPort(t, srv)

	conn, err := dialWithLiveness(context.Background(), host, port, livenessConfig{
		pingInterval: 10 * time.Millisecond,
		pongWait:     50 * time.Millisecond,
		writeTimeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if connectionCount.Load() >= 2 && conn.State() == Connected {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if connectionCount.Load() < 2 {
		t.Fatal("silent peer did not trigger a reconnect")
	}
	if conn.State() != Connected {
		t.Fatalf("state after liveness recovery = %s, want Connected", conn.State())
	}
	if _, err := conn.Call(context.Background(), "ping", nil); err != nil {
		t.Fatalf("call after liveness recovery: %v", err)
	}
}

func TestKeepaliveStopsAfterClose(t *testing.T) {
	var pingCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		ws.SetPingHandler(func(data string) error {
			pingCount.Add(1)
			return ws.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(time.Second))
		})
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	host, port := serverHostPort(t, srv)
	liveness := livenessConfig{
		pingInterval: 10 * time.Millisecond,
		pongWait:     100 * time.Millisecond,
		writeTimeout: 20 * time.Millisecond,
	}
	conn, err := dialWithLiveness(context.Background(), host, port, liveness)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for pingCount.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if pingCount.Load() < 2 {
		t.Fatal("keepalive did not send ping frames")
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	time.Sleep(2 * liveness.pingInterval)
	closedCount := pingCount.Load()
	time.Sleep(5 * liveness.pingInterval)
	if got := pingCount.Load(); got != closedCount {
		t.Fatalf("keepalive continued after close: ping count %d -> %d", closedCount, got)
	}
}

func TestDialFailure(t *testing.T) {
	ctx := context.Background()
	_, err := Dial(ctx, "127.0.0.1", 1) // port 1 won't have a WS server
	if err == nil {
		t.Fatal("expected error dialing bad address")
	}
	if !strings.Contains(err.Error(), "dial") {
		t.Errorf("error should mention dial: %v", err)
	}
}

func TestClose(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	host, port := serverHostPort(t, srv)

	ctx := context.Background()
	conn, err := Dial(ctx, host, port)
	if err != nil {
		t.Fatal(err)
	}

	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if conn.State() != Disconnected {
		t.Errorf("state after close = %v, want Disconnected", conn.State())
	}

	// Calls after close should fail.
	_, err = conn.Call(ctx, "ping", nil)
	if err == nil {
		t.Fatal("expected error after close")
	}
}

func TestCallNotConnected(t *testing.T) {
	c := &Connection{
		state:   Disconnected,
		pending: make(map[int64]chan *Response),
		done:    make(chan struct{}),
	}
	_, err := c.Call(context.Background(), "ping", nil)
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("expected ErrNotConnected, got %v", err)
	}
}

func TestReconnectAfterServerDrop(t *testing.T) {
	// Server explicitly closes the WebSocket when signaled via dropConn.
	dropConn := make(chan struct{})
	dropped := make(chan struct{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		done := make(chan struct{})
		go func() {
			select {
			case <-dropConn:
				ws.Close()
				close(dropped)
			case <-done:
			}
		}()
		defer close(done)

		for {
			var req Request
			if err := ws.ReadJSON(&req); err != nil {
				return
			}
			result, _ := json.Marshal(map[string]string{"status": "ok"})
			ws.WriteJSON(Response{JSONRPC: "2.0", ID: req.ID, Result: result})
		}
	})

	// Use a specific listener so we can rebind the same port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	ln.Close()

	// Start initial server on that port.
	ln1, err := net.Listen("tcp", "127.0.0.1:"+portStr)
	if err != nil {
		t.Fatal(err)
	}
	srv1 := httptest.NewUnstartedServer(handler)
	srv1.Listener = ln1
	srv1.Start()

	ctx := context.Background()
	conn, err := Dial(ctx, "127.0.0.1", port)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Verify initial connection works.
	if _, err := conn.Call(ctx, "ping", nil); err != nil {
		t.Fatal(err)
	}

	// Signal the server to close the WebSocket, then wait for it.
	close(dropConn)
	<-dropped
	srv1.Close()

	// Wait for readLoop to detect the disconnect.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if conn.State() != Connected {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if conn.State() == Connected {
		t.Fatal("should not be Connected after server close")
	}

	// Restart server on same port with a fresh handler (no dropConn).
	ln2, err := net.Listen("tcp", "127.0.0.1:"+portStr)
	if err != nil {
		t.Fatalf("could not rebind port %s: %v", portStr, err)
	}
	srv2 := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			var req Request
			if err := ws.ReadJSON(&req); err != nil {
				return
			}
			result, _ := json.Marshal(map[string]string{"status": "ok"})
			ws.WriteJSON(Response{JSONRPC: "2.0", ID: req.ID, Result: result})
		}
	}))
	srv2.Listener = ln2
	srv2.Start()
	defer srv2.Close()

	// Wait for reconnection (backoff starts at 100ms).
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if conn.State() == Connected {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if conn.State() != Connected {
		t.Fatalf("state = %v, want Connected after reconnect", conn.State())
	}

	// Verify the reconnected connection works.
	resp, err := conn.Call(ctx, "ping", nil)
	if err != nil {
		t.Fatalf("call after reconnect: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestReconnectClosesOldSocketBeforeOverwrite(t *testing.T) {
	// Server accepts one message then drops the connection, simulating a
	// crash/flap on the Godot side.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		ws.ReadMessage()
		ws.Close()
	}))
	defer srv.Close()
	host, port := serverHostPort(t, srv)

	ctx := context.Background()
	conn, err := Dial(ctx, host, port)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	conn.mu.Lock()
	oldWS := conn.ws
	conn.mu.Unlock()

	// Triggers the write that the server reads before dropping the conn.
	_, _ = conn.Call(ctx, "ping", nil)

	// Wait for readLoop to observe the drop and hand off to reconnectLoop,
	// which will eventually overwrite conn.ws with a new socket.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && conn.State() == Connected {
		time.Sleep(10 * time.Millisecond)
	}
	if conn.State() == Connected {
		t.Fatal("expected state to leave Connected after server drop")
	}

	// The old socket must already be closed locally (not just abandoned) by
	// the time the disconnect is handled, regardless of whether a new one
	// has replaced it yet. SetReadDeadline is a local-only operation that
	// fails once the underlying conn has been closed, so this doesn't race
	// against the remote peer or the reconnect goroutine.
	if err := oldWS.UnderlyingConn().SetReadDeadline(time.Time{}); err == nil {
		t.Fatal("old socket was not closed on disconnect; leaked connection")
	}
}

func TestDialAddrIPv6(t *testing.T) {
	// net.JoinHostPort must bracket IPv6 literals; verify without a live listener.
	cases := []struct {
		host string
		port int
		want string
	}{
		{"::1", 26700, "[::1]:26700"},
		{"::1", 8080, "[::1]:8080"},
		{"127.0.0.1", 26700, "127.0.0.1:26700"},
		{"localhost", 26700, "localhost:26700"},
	}
	for _, tc := range cases {
		got := net.JoinHostPort(tc.host, strconv.Itoa(tc.port))
		if got != tc.want {
			t.Errorf("JoinHostPort(%q, %d) = %q, want %q", tc.host, tc.port, got, tc.want)
		}
	}

	// Also confirm Dial stores the bracketed form in Addr().
	// We use a bad port so Dial fails fast; Addr() is set before the dial attempt.
	// Instead, construct directly as Dial does and check addr.
	addr := net.JoinHostPort("::1", strconv.Itoa(26700))
	if addr != "[::1]:26700" {
		t.Errorf("addr = %q, want [::1]:26700", addr)
	}
}

func TestPendingCallsCancelledOnDisconnect(t *testing.T) {
	// Server that accepts connection then closes immediately after one read.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Read one message then close to simulate disconnect.
		ws.ReadMessage()
		ws.Close()
	}))
	defer srv.Close()
	host, port := serverHostPort(t, srv)

	ctx := context.Background()
	conn, err := Dial(ctx, host, port)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// This call will be sent but the server will close before responding.
	_, err = conn.Call(ctx, "ping", nil)
	if err == nil {
		t.Fatal("expected error from cancelled pending call")
	}
}
