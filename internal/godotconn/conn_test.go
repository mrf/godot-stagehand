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
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

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
		t.Skipf("could not rebind port %s: %v", portStr, err)
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
