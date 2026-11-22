package godotconn

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mrf/godot-stagehand/internal/gwp"
	"github.com/mrf/godot-stagehand/internal/gwp/gwptest"
)

func TestBackoffDuration(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 100 * time.Millisecond},
		{1, 200 * time.Millisecond},
		{2, 400 * time.Millisecond},
		{3, 800 * time.Millisecond},
		{4, 1600 * time.Millisecond},
		{5, 3200 * time.Millisecond},
		{6, 5 * time.Second}, // capped
		{7, 5 * time.Second}, // stays capped
		{100, 5 * time.Second},
	}

	for _, tt := range tests {
		got := backoffDuration(tt.attempt)
		if got != tt.want {
			t.Errorf("backoffDuration(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestStateString(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{Disconnected, "Disconnected"},
		{Connecting, "Connecting"},
		{Connected, "Connected"},
		{Reconnecting, "Reconnecting"},
		{State(99), "State(99)"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", int(tt.state), got, tt.want)
		}
	}
}

func TestConfiguredMaxReconnectAttempts(t *testing.T) {
	tests := []struct {
		name string
		env  string
		set  bool
		want int
	}{
		{"unset uses bounded default", "", false, defaultMaxReconnectAttempts},
		{"explicit zero means unlimited", "0", true, 0},
		{"positive override", "5", true, 5},
		{"negative falls back to default", "-1", true, defaultMaxReconnectAttempts},
		{"garbage falls back to default", "banana", true, defaultMaxReconnectAttempts},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv(maxReconnectAttemptsEnv, tt.env)
			}
			if got := configuredMaxReconnectAttempts(); got != tt.want {
				t.Errorf("configuredMaxReconnectAttempts() = %d, want %d", got, tt.want)
			}
		})
	}
}

// reauthBehavior is how reauthServer answers the authenticate call on a
// reconnect (every connection after the first).
type reauthBehavior int

const (
	reauthOK     reauthBehavior = iota // confirm the token
	reauthReject                       // AUTHENTICATION_FAILED, then close — what the addon does
	reauthStall                        // never answer: a scene load / shader compile stall
	reauthDrop                         // close the socket without answering
)

// reauthServer accepts the initial connection's authenticate and drops it on
// demand; each later connection n (2, 3, ...) answers authenticate as
// behavior(n) says. It counts connections so a test can tell one reconnect
// loop from several racing ones.
type reauthServer struct {
	*httptest.Server
	connections atomic.Int32
	first       chan *websocket.Conn
}

func newReauthServer(t *testing.T, behavior func(n int32) reauthBehavior) *reauthServer {
	t.Helper()
	s := &reauthServer{first: make(chan *websocket.Conn, 1)}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		n := s.connections.Add(1)
		if n == 1 {
			s.first <- ws
		}
		for {
			var req Request
			if err := ws.ReadJSON(&req); err != nil {
				return
			}
			if req.Method == "authenticate" && n > 1 {
				switch behavior(n) {
				case reauthReject:
					_ = ws.WriteJSON(Response{
						JSONRPC: "2.0",
						ID:      req.ID,
						Error:   &RPCError{Code: CodeAuthenticationFailed, Message: "Authentication failed"},
					})
					return
				case reauthStall:
					continue
				case reauthDrop:
					return
				case reauthOK:
				}
			}
			result, _ := json.Marshal(map[string]bool{"authenticated": true})
			if err := ws.WriteJSON(Response{JSONRPC: "2.0", ID: req.ID, Result: result}); err != nil {
				return
			}
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// dialAuthenticatedThenDrop dials s, authenticates, then has the server drop
// the initial socket so the client enters its reconnect loop.
func dialAuthenticatedThenDrop(t *testing.T, s *reauthServer) *Connection {
	t.Helper()
	host, port := serverHostPort(t, s.Server)
	conn, err := dialWithLimits(context.Background(), host, port, defaultLiveness, 5)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.Authenticate(context.Background(), testConnectionAuthToken); err != nil {
		t.Fatalf("authenticate initial connection: %v", err)
	}
	if err := (<-s.first).Close(); err != nil {
		t.Fatalf("drop first connection: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for conn.State() == Connected && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if conn.State() == Connected {
		t.Fatal("connection never noticed the dropped socket")
	}
	return conn
}

func waitForState(t *testing.T, conn *Connection, want State, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if conn.State() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("state = %v after %v, want %v", conn.State(), timeout, want)
}

// assertSettled checks that, once the reconnect has resolved, nothing is still
// dialling behind it: a racing second loop shows up as extra connections.
func assertSettled(t *testing.T, conn *Connection, s *reauthServer, wantState State, wantConnections int32) {
	t.Helper()
	time.Sleep(time.Second) // several 100ms-backoff dial cycles, were a stray loop running
	if got := conn.State(); got != wantState {
		t.Errorf("state settled at %v, want %v", got, wantState)
	}
	if got := s.connections.Load(); got != wantConnections {
		t.Errorf("server saw %d connections, want %d (a stray reconnect loop kept dialling)", got, wantConnections)
	}
}

func TestReconnectSurvivesReauthTimeout(t *testing.T) {
	s := newReauthServer(t, func(n int32) reauthBehavior {
		if n == 2 {
			return reauthStall
		}
		return reauthOK
	})
	conn := dialAuthenticatedThenDrop(t, s)

	waitForState(t, conn, Connected, queueTimeout+3*time.Second)
	if conn.ReconnectExhausted() {
		t.Error("ReconnectExhausted() = true: a re-auth timeout gave up instead of retrying")
	}
	if _, err := conn.Call(context.Background(), "ping", nil); err != nil {
		t.Errorf("call after recovered reconnect: %v", err)
	}
	assertSettled(t, conn, s, Connected, 3)
}

func TestReconnectSurvivesDropDuringReauth(t *testing.T) {
	s := newReauthServer(t, func(n int32) reauthBehavior {
		if n == 2 {
			return reauthDrop
		}
		return reauthOK
	})
	conn := dialAuthenticatedThenDrop(t, s)

	waitForState(t, conn, Connected, 3*time.Second)
	if conn.ReconnectExhausted() {
		t.Error("ReconnectExhausted() = true: a mid-handshake drop gave up instead of retrying")
	}
	assertSettled(t, conn, s, Connected, 3)
}

func TestReconnectGivesUpOnTokenRejection(t *testing.T) {
	s := newReauthServer(t, func(int32) reauthBehavior { return reauthReject })
	conn := dialAuthenticatedThenDrop(t, s)

	waitForState(t, conn, Disconnected, 3*time.Second)
	if !conn.ReconnectExhausted() {
		t.Error("ReconnectExhausted() = false after the token was rejected")
	}
	assertSettled(t, conn, s, Disconnected, 2)
}

// handshakeServer authenticates every socket and answers ping with the
// handshake handshakeFor returns for that socket's 1-based connection number.
// Each accepted socket is sent on the returned channel so a test can drop it.
func handshakeServer(
	t *testing.T,
	handshakeFor func(connNumber int) json.RawMessage,
) (*httptest.Server, <-chan *websocket.Conn) {
	t.Helper()
	sockets := make(chan *websocket.Conn, 16)
	var count atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		n := int(count.Add(1))
		sockets <- ws
		for {
			var req Request
			if err := ws.ReadJSON(&req); err != nil {
				return
			}
			var result json.RawMessage
			switch req.Method {
			case "authenticate":
				result = json.RawMessage(`{"authenticated":true}`)
			case "ping":
				result = handshakeFor(n)
			default:
				result = json.RawMessage(`{}`)
			}
			_ = ws.WriteJSON(Response{JSONRPC: "2.0", ID: req.ID, Result: result})
		}
	}))
	t.Cleanup(srv.Close)
	return srv, sockets
}

// pinnedConnection dials srv, authenticates, negotiates, and pins the result.
func pinnedConnection(t *testing.T, srv *httptest.Server) *Connection {
	t.Helper()
	host, port := serverHostPort(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := dialWithLimits(ctx, host, port, defaultLiveness, 3)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.Authenticate(ctx, testConnectionAuthToken); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	resp, err := conn.Call(ctx, "ping", nil)
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	info, err := gwp.Negotiate(resp.Result)
	if err != nil {
		t.Fatalf("negotiate: %v", err)
	}
	conn.PinHandshake(info)
	return conn
}

func TestReconnectRejectsDifferentInstanceToken(t *testing.T) {
	srv, sockets := handshakeServer(t, func(n int) json.RawMessage {
		token := "game-A"
		if n > 1 {
			token = "game-B"
		}
		return gwptest.Handshake(map[string]any{"instance_token": token})
	})
	conn := pinnedConnection(t, srv)

	_ = (<-sockets).Close()
	waitForState(t, conn, Disconnected, 5*time.Second)
	if !conn.ReconnectExhausted() {
		t.Fatal("a reconnect to a different instance must be a permanent give-up")
	}
	if _, err := conn.Call(context.Background(), "ping", nil); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("Call after rejected reconnect = %v, want ErrNotConnected", err)
	}
}

func TestReconnectRejectsProtocolMismatch(t *testing.T) {
	srv, sockets := handshakeServer(t, func(n int) json.RawMessage {
		if n > 1 {
			return gwptest.Handshake(map[string]any{"protocol_version": gwp.ProtocolVersion + 1})
		}
		return gwptest.Handshake(nil)
	})
	conn := pinnedConnection(t, srv)

	_ = (<-sockets).Close()
	waitForState(t, conn, Disconnected, 5*time.Second)
	if !conn.ReconnectExhausted() {
		t.Fatal("a protocol-mismatched peer must be a permanent give-up")
	}
}

func TestReconnectAcceptsSameInstance(t *testing.T) {
	srv, sockets := handshakeServer(t, func(int) json.RawMessage { return gwptest.Handshake(nil) })
	conn := pinnedConnection(t, srv)

	_ = (<-sockets).Close()
	for conn.State() == Connected {
		time.Sleep(5 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := conn.Call(ctx, "ping", nil); err != nil {
		t.Fatalf("call after same-instance reconnect: %v", err)
	}
	if conn.ReconnectExhausted() {
		t.Fatal("same instance must not be rejected")
	}
}
