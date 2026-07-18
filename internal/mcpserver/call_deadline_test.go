package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mrf/godot-stagehand/internal/godotconn"
)

func TestOrdinaryGodotCallUsesDefaultDeadline(t *testing.T) {
	host, port, respond := deadlineStubGodot(t)
	srv := New()
	srv.callTimeout = 50 * time.Millisecond
	t.Cleanup(srv.clearConn)
	connectDeadlineStub(t, srv, host, port)

	resultCh := make(chan *mcp.CallToolResult, 1)
	started := time.Now()
	go func() {
		result, _ := srv.handleGetTree(context.Background(), toolReq(nil))
		resultCh <- result
	}()

	select {
	case result := <-resultCh:
		resultText := mustText(t, result)
		elapsed := time.Since(started)
		if elapsed < 40*time.Millisecond || elapsed > 500*time.Millisecond {
			t.Fatalf("default deadline returned after %s, want about 50ms", elapsed)
		}
		if !strings.Contains(resultText, "timed out after 50ms") {
			t.Fatalf("timeout error lacks configured deadline: %s", resultText)
		}
	case <-time.After(750 * time.Millisecond):
		t.Fatal("ordinary Godot call ignored the default deadline")
	}

	// A timed-out request must not poison the connection or response routing.
	respond.Store(true)
	result, _ := srv.handleGetTree(context.Background(), toolReq(nil))
	if result.IsError {
		t.Fatalf("call after timeout did not recover: %s", mustText(t, result))
	}
}

func TestCallerDeadlineOverridesOrdinaryCallDefault(t *testing.T) {
	host, port, _ := deadlineStubGodot(t)
	srv := New()
	srv.callTimeout = 20 * time.Millisecond
	t.Cleanup(srv.clearConn)
	connectDeadlineStub(t, srv, host, port)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, result := srv.callGodotInstance(ctx, "default", "get_tree", nil)
	if result == nil || !result.IsError {
		t.Fatal("silent call should return a deadline error")
	}
	if elapsed := time.Since(started); elapsed < 80*time.Millisecond {
		t.Fatalf("default deadline shortened caller override: returned after %s", elapsed)
	}
}

func TestConnectUsesDefaultDeadline(t *testing.T) {
	release := make(chan struct{})
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		var request godotconn.Request
		if err := ws.ReadJSON(&request); err != nil {
			return
		}
		<-release
	}))
	t.Cleanup(func() {
		close(release)
		stub.Close()
	})
	host, port := serverHostPort(t, stub)
	srv := New()
	srv.callTimeout = 50 * time.Millisecond

	resultCh := make(chan *mcp.CallToolResult, 1)
	go func() {
		result, _ := srv.handleConnect(context.Background(), toolReq(map[string]any{
			"host":       host,
			"port":       float64(port),
			"auth_token": testMCPAuthToken,
		}))
		resultCh <- result
	}()
	select {
	case result := <-resultCh:
		if result == nil || !result.IsError {
			t.Fatal("silent authentication should return a timeout error")
		}
		if text := mustText(t, result); !strings.Contains(text, "50ms") {
			t.Fatalf("connect timeout lacks configured deadline: %s", text)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("godot_connect ignored the default deadline")
	}
}

func TestOrdinaryCallDeadlineCanBeConfigured(t *testing.T) {
	t.Setenv("STAGEHAND_CALL_TIMEOUT_MS", "45000")
	if got := New().callTimeout; got != 45*time.Second {
		t.Fatalf("configured call timeout = %s, want 45s", got)
	}

	for _, invalid := range []string{"0", "-1", "86400001", "not-a-number"} {
		t.Run(invalid, func(t *testing.T) {
			t.Setenv("STAGEHAND_CALL_TIMEOUT_MS", invalid)
			if got := New().callTimeout; got != defaultGodotCallTimeout {
				t.Fatalf("invalid override %q produced %s, want default %s", invalid, got, defaultGodotCallTimeout)
			}
		})
	}
}

func TestOrdinaryCallDeadlineIsDocumented(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	for _, want := range []string{
		"STAGEHAND_CALL_TIMEOUT_MS",
		"30 seconds",
		"timeout_ms",
		"ping every 10 seconds",
		"pong",
	} {
		if !strings.Contains(string(readme), want) {
			t.Fatalf("README does not document ordinary/wait call deadlines: missing %q", want)
		}
	}
}

func deadlineStubGodot(t *testing.T) (string, int, *atomic.Bool) {
	t.Helper()
	respond := &atomic.Bool{}
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			var req godotconn.Request
			if err := ws.ReadJSON(&req); err != nil {
				return
			}
			var result json.RawMessage
			switch req.Method {
			case "authenticate":
				result = json.RawMessage(`{"authenticated":true}`)
			case "ping":
				result = json.RawMessage(`{"status":"ok","engine":"godot"}`)
			default:
				if !respond.Load() {
					continue
				}
				result = json.RawMessage(`{"nodes":[]}`)
			}
			if err := ws.WriteJSON(godotconn.Response{JSONRPC: "2.0", ID: req.ID, Result: result}); err != nil {
				return
			}
		}
	}))
	t.Cleanup(stub.Close)
	host, port := serverHostPort(t, stub)
	return host, port, respond
}

func connectDeadlineStub(t *testing.T, srv *Server, host string, port int) {
	t.Helper()
	result, err := srv.handleConnect(context.Background(), toolReq(map[string]any{
		"host":       host,
		"port":       float64(port),
		"auth_token": testMCPAuthToken,
	}))
	if err != nil {
		t.Fatalf("connect deadline stub: %v", err)
	}
	if result.IsError {
		t.Fatalf("connect deadline stub: %s", mustText(t, result))
	}
}
