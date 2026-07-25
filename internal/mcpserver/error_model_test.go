package mcpserver

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mrf/godot-stagehand/internal/godotconn"
)

// These tests pin the agent-facing half of the canonical error model
// (godot-stagehand-vv2.8, docs/error-model.md): every way a Godot call can fail
// must reach the caller as an isError tool result whose text names the method,
// the selector it targeted, the machine-readable kind, and what to do next.
// The failure this guards against is the quiet one — a failed call arriving as
// a successful tool result that an agent then reads as "it worked".

// errorText asserts that result is an MCP error result and returns its text.
func errorText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result == nil {
		t.Fatal("expected an error tool result, got nil")
	}
	if !result.IsError {
		t.Fatalf("expected IsError result, got success: %+v", result)
	}
	if len(result.Content) == 0 {
		t.Fatal("error result carries no content")
	}
	text, ok := mcp.AsTextContent(result.Content[0])
	if !ok {
		t.Fatalf("error result content is not text: %+v", result.Content[0])
	}
	return text.Text
}

// errorData builds the `error.data` payload the addon attaches to a handler
// failure. It mirrors StagehandJsonRpc.make_handler_error_response.
func errorData(t *testing.T, code, method, selector string, details map[string]any) json.RawMessage {
	t.Helper()
	payload := map[string]any{"error_code": code, "method": method}
	if selector != "" {
		payload["selector"] = selector
	}
	if len(details) > 0 {
		payload["details"] = details
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestErrorModel_DisconnectedInstance covers the failure that happens before a
// call is even attempted: no session for the requested instance.
func TestErrorModel_DisconnectedInstance(t *testing.T) {
	srv := New()
	t.Cleanup(srv.clearConn)

	t.Run("default instance", func(t *testing.T) {
		result, err := srv.handleGetProperty(context.Background(), toolReq(map[string]any{
			"selector": "name:Player",
			"property": "position",
		}))
		if err != nil {
			t.Fatalf("handler returned a Go error: %v", err)
		}
		assertContainsAll(t, errorText(t, result), []string{"Not connected", "godot_connect", "godot_launch"})
	})

	t.Run("named instance", func(t *testing.T) {
		result, err := srv.handleGetProperty(context.Background(), toolReq(map[string]any{
			"instance_id": "player-two",
			"selector":    "name:Player",
			"property":    "position",
		}))
		if err != nil {
			t.Fatalf("handler returned a Go error: %v", err)
		}
		// The instance id must appear, otherwise a caller juggling several
		// sessions cannot tell which one it forgot to connect.
		assertContainsAll(t, errorText(t, result), []string{`"player-two"`, "godot_connect"})
	})
}

// TestErrorModel_InvalidSelector covers the failure caught in Go, before the
// request goes on the wire.
func TestErrorModel_InvalidSelector(t *testing.T) {
	srv, stub := setupE2ETest(t)

	result, err := srv.handleGetProperty(context.Background(), toolReq(map[string]any{
		"selector": "name:Player >> ",
		"property": "position",
	}))
	if err != nil {
		t.Fatalf("handler returned a Go error: %v", err)
	}
	assertContainsAll(t, errorText(t, result), []string{"invalid selector"})

	if n := stub.callCount("get_property"); n != 0 {
		t.Errorf("an invalid selector must be rejected locally, but %d get_property call(s) reached Godot", n)
	}
}

// TestErrorModel_MissingHandler covers a method the addon does not implement.
// It stays a JSON-RPC -32601, distinct from a handler that ran and failed.
func TestErrorModel_MissingHandler(t *testing.T) {
	srv, stub := setupE2ETest(t)
	stub.replyError("get_property", &godotconn.RPCError{
		Code:    godotconn.CodeMethodNotFound,
		Message: "Method not found: get_property",
	})

	result, err := srv.handleGetProperty(context.Background(), toolReq(map[string]any{
		"selector": "name:Player",
		"property": "position",
	}))
	if err != nil {
		t.Fatalf("handler returned a Go error: %v", err)
	}
	// No structured data: an addon that never registered the method cannot
	// describe the failure, so the method this server called fills the gap.
	assertContainsAll(t, errorText(t, result), []string{"get_property failed", "Method not found"})
}

// TestErrorModel_HandlerFailures covers the addon-side failures that reach the
// caller as canonical JSON-RPC errors with structured data attached.
func TestErrorModel_HandlerFailures(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		rpcErr   *godotconn.RPCError
		call     func(*Server) (*mcp.CallToolResult, error)
		contains []string
	}{
		{
			name:   "missing node",
			method: "get_property",
			rpcErr: &godotconn.RPCError{
				Code:    godotconn.CodeTargetNotFound,
				Message: "Node not found for selector: name:Ghost",
			},
			call: func(s *Server) (*mcp.CallToolResult, error) {
				return s.handleGetProperty(context.Background(), toolReq(map[string]any{
					"selector": "name:Ghost",
					"property": "position",
				}))
			},
			contains: []string{
				"get_property failed",
				"Node not found for selector: name:Ghost",
				"code=node_not_found",
				`selector="name:Ghost"`,
				"Call get_tree or query_nodes",
			},
		},
		{
			name:   "bad property",
			method: "set_property",
			rpcErr: &godotconn.RPCError{
				Code:    godotconn.CodeTargetNotFound,
				Message: "Property not found: hitpoints",
			},
			call: func(s *Server) (*mcp.CallToolResult, error) {
				return s.handleSetProperty(context.Background(), toolReq(map[string]any{
					"selector": "name:Player",
					"property": "hitpoints",
					"value":    "10",
				}))
			},
			contains: []string{
				"set_property failed",
				"Property not found: hitpoints",
				"code=property_not_found",
				`selector="name:Player"`,
				"list the properties this node exposes",
				// Remaining details are rendered too, so the caller sees the
				// class it was actually talking to.
				"node_class=CharacterBody2D",
			},
		},
		{
			name:   "addon-side timeout",
			method: "wait_signal",
			rpcErr: &godotconn.RPCError{
				Code:    godotconn.CodeTimeout,
				Message: "Signal 'died' was not emitted before timeout (selector: name:Player, timeout: 500ms)",
			},
			call: func(s *Server) (*mcp.CallToolResult, error) {
				return s.handleWaitForSignal(context.Background(), toolReq(map[string]any{
					"selector":    "name:Player",
					"signal_name": "died",
					"timeout_ms":  float64(500),
				}))
			},
			contains: []string{
				"wait_signal failed",
				"was not emitted before timeout",
				"code=timeout",
				`selector="name:Player"`,
				"Raise timeout_ms",
			},
		},
	}

	details := map[string]map[string]any{
		"get_property": {"next_action": "Call get_tree or query_nodes to confirm the node exists."},
		"set_property": {
			"next_action": "Call get_tree with the properties argument to list the properties this node exposes.",
			"node_class":  "CharacterBody2D",
		},
		"wait_signal": {
			"next_action": "Raise timeout_ms, or drive the game state that emits this signal.",
			"timeout_ms":  float64(500),
		},
	}
	selectors := map[string]string{
		"get_property": "name:Ghost",
		"set_property": "name:Player",
		"wait_signal":  "name:Player",
	}
	codes := map[string]string{
		"get_property": "node_not_found",
		"set_property": "property_not_found",
		"wait_signal":  "timeout",
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, stub := setupE2ETest(t)
			tc.rpcErr.Data = errorData(t,
				codes[tc.method], tc.method, selectors[tc.method], details[tc.method])
			stub.replyError(tc.method, tc.rpcErr)

			result, err := tc.call(srv)
			if err != nil {
				t.Fatalf("handler returned a Go error: %v", err)
			}
			assertContainsAll(t, errorText(t, result), tc.contains)
		})
	}
}

// TestErrorModel_CallTimeout covers the Go-side deadline: Godot accepted the
// request and never answered. The advice must point at the game, not the call.
func TestErrorModel_CallTimeout(t *testing.T) {
	// A stub that accepts the connection and never answers the wait: alive on
	// TCP, so no disconnect fires and only the Go-side deadline can end the call.
	host, port, _ := blockingStubGodot(t, false)

	srv := New()
	t.Cleanup(srv.clearConn)
	srv.callTimeout = 100 * time.Millisecond
	connResult, err := srv.handleConnect(context.Background(), toolReq(map[string]any{
		"host":       host,
		"port":       float64(port),
		"auth_token": testMCPAuthToken,
	}))
	if err != nil || connResult.IsError {
		t.Fatalf("connect failed: err=%v result=%+v", err, connResult)
	}

	result, err := srv.handleWaitForNode(context.Background(), toolReq(map[string]any{
		"selector":   "name:Player",
		"timeout_ms": float64(20),
	}))
	if err != nil {
		t.Fatalf("handler returned a Go error: %v", err)
	}
	assertContainsAll(t, errorText(t, result), []string{
		`"wait_for_node" timed out`, "godot_status", "STAGEHAND_CALL_TIMEOUT_MS"})
}

// TestErrorModel_LegacyAddonInResultError covers an addon vendored into a host
// project before the canonical error model: it reports failures inside an
// otherwise successful result. Those must still surface as isError, or the
// upgrade would silently turn old failures into successes.
func TestErrorModel_LegacyAddonInResultError(t *testing.T) {
	errResult := checkGodotResult(json.RawMessage(
		`{"error":"Node not found for selector: name:Ghost","error_code":"no_match","details":{"selector":"name:Ghost"}}`))
	if errResult == nil {
		t.Fatal("a legacy in-result error must still produce an isError tool result")
	}
	assertContainsAll(t, errorText(t, errResult), []string{
		"Node not found for selector: name:Ghost", "code=no_match"})
}
