package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mrf/godot-stagehand/internal/godotconn"
	"github.com/mrf/godot-stagehand/internal/gwp"
	"github.com/mrf/godot-stagehand/internal/gwp/gwptest"
)

// currentHandshakeJSON is the ping result a current-generation addon returns.
// Every stub Godot in this package must answer ping with it, otherwise
// godot_connect correctly refuses the stub as a pre-negotiation addon.
func currentHandshakeJSON(overrides map[string]any) string {
	return string(gwptest.Handshake(overrides))
}

// connectWithHandshake runs godot_connect against a stub whose ping returns the
// supplied payload verbatim.
func connectWithHandshake(t *testing.T, pingResult string) *mcp.CallToolResult {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			var req godotconn.Request
			if err := ws.ReadJSON(&req); err != nil {
				return
			}
			resp := godotconn.Response{JSONRPC: "2.0", ID: req.ID}
			switch req.Method {
			case "authenticate":
				resp.Result = json.RawMessage(`{"authenticated":true}`)
			case "ping":
				resp.Result = json.RawMessage(pingResult)
			default:
				resp.Result = json.RawMessage(`{}`)
			}
			if err := ws.WriteJSON(resp); err != nil {
				return
			}
		}
	}))
	t.Cleanup(stub.Close)

	host, port := serverHostPort(t, stub)
	srv := New()
	t.Cleanup(srv.clearConn)
	result, err := srv.handleConnect(context.Background(), toolReq(map[string]any{
		"host":       host,
		"port":       float64(port),
		"auth_token": testMCPAuthToken,
	}))
	if err != nil {
		t.Fatalf("handleConnect error: %v", err)
	}
	return result
}

// TestConnectRejectsIncompatibleProtocol pins the tool-level behaviour the
// handshake negotiation exists for: an addon from another GWP generation is
// refused at godot_connect with an actionable message, rather than admitted and
// then failing opaquely on some later tool call.
func TestConnectRejectsIncompatibleProtocol(t *testing.T) {
	cases := []struct {
		name       string
		ping       string
		wantPhrase string
	}{
		{
			name:       "pre-negotiation addon",
			ping:       `{"status":"ok","engine":"godot","stagehand_version":"0.1.0"}`,
			wantPhrase: "setup --force",
		},
		{
			name:       "newer addon",
			ping:       currentHandshakeJSON(map[string]any{"protocol_version": gwp.ProtocolVersion + 1}),
			wantPhrase: "godot-stagehand binary",
		},
		{
			name: "missing required capability",
			ping: currentHandshakeJSON(map[string]any{
				"capabilities": []string{gwp.CapabilityCore, gwp.CapabilityInput, gwp.CapabilityWait},
			}),
			wantPhrase: gwp.CapabilityScreenshot,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := connectWithHandshake(t, testCase.ping)
			if !result.IsError {
				t.Fatalf("godot_connect succeeded against an incompatible addon: %+v", result)
			}
			if text := toolResultText(t, result); !strings.Contains(text, testCase.wantPhrase) {
				t.Errorf("connect error %q does not contain %q", text, testCase.wantPhrase)
			}
		})
	}
}

// TestConnectAcceptsCompatibleHandshake covers the fully-compatible path and
// the degraded-but-usable path (older addon, fewer optional capabilities, mixed
// build versions) that must still connect.
func TestConnectAcceptsCompatibleHandshake(t *testing.T) {
	result := connectWithHandshake(t, currentHandshakeJSON(nil))
	if result.IsError {
		t.Fatalf("godot_connect rejected a compatible addon: %s", toolResultText(t, result))
	}
	if text := toolResultText(t, result); !strings.Contains(text, gwp.ProtocolID) {
		t.Errorf("connect result %q does not report the negotiated protocol", text)
	}

	degraded := connectWithHandshake(t, currentHandshakeJSON(map[string]any{
		"capabilities":      gwp.RequiredCapabilities,
		"stagehand_version": "0.0.1",
	}))
	if degraded.IsError {
		t.Fatalf("godot_connect rejected an addon missing only optional capabilities: %s", toolResultText(t, degraded))
	}
	text := toolResultText(t, degraded)
	if !strings.Contains(text, gwp.CapabilityRecording) {
		t.Errorf("connect result %q does not name the unavailable capability", text)
	}
	if !strings.Contains(text, "0.0.1") {
		t.Errorf("connect result %q does not report the addon version skew", text)
	}
}

func toolResultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	var builder strings.Builder
	for _, content := range result.Content {
		text, ok := mcp.AsTextContent(content)
		if !ok {
			continue
		}
		builder.WriteString(text.Text)
	}
	return builder.String()
}
