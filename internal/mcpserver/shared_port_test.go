package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestConnectToolDescriptionWarnsAboutSharedDefaultPort(t *testing.T) {
	description := connectTool.Description
	for _, want := range []string{"26700", "shared", "godot_launch"} {
		if !strings.Contains(description, want) {
			t.Fatalf("godot_connect description must mention %q: %q", want, description)
		}
	}

	portProperty, ok := connectTool.InputSchema.Properties["port"]
	if !ok {
		t.Fatal("godot_connect schema must expose port")
	}
	portSchema, ok := portProperty.(map[string]any)
	if !ok {
		t.Fatalf("godot_connect port schema has type %T, want map[string]any", portProperty)
	}
	portDescription, _ := portSchema["description"].(string)
	if !strings.Contains(portDescription, "shared") {
		t.Fatalf("godot_connect port description must flag the shared default: %q", portDescription)
	}
}

func TestLaunchToolDescriptionPromotesPrivateInstance(t *testing.T) {
	description := launchTool.Description
	for _, want := range []string{"private", "godot_connect"} {
		if !strings.Contains(description, want) {
			t.Fatalf("godot_launch description must mention %q: %q", want, description)
		}
	}
}

func TestConnectRequiresExplicitPortInMultiInstanceMode(t *testing.T) {
	t.Setenv(multiInstanceEnvVar, "1")
	s := New()

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"auth_token": testMCPAuthToken}
	result, err := s.handleConnect(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected isError=true when port is omitted in multi-instance mode")
	}
	text, ok := mcp.AsTextContent(result.Content[0])
	if !ok {
		t.Fatal("expected TextContent")
	}
	for _, want := range []string{multiInstanceEnvVar, "port", "godot_launch"} {
		if !strings.Contains(text.Text, want) {
			t.Fatalf("multi-instance gate message must mention %q: %q", want, text.Text)
		}
	}
}

func TestConnectAcceptsExplicitPortInMultiInstanceMode(t *testing.T) {
	t.Setenv(multiInstanceEnvVar, "1")
	s := New()

	req := mcp.CallToolRequest{}
	// Port 1 is privileged and unbound in the test environment, so the dial fails;
	// the point is that it fails at dial time rather than at the explicit-port gate.
	req.Params.Arguments = map[string]any{"auth_token": testMCPAuthToken, "port": float64(1)}
	result, err := s.handleConnect(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected isError=true because nothing is listening on port 1")
	}
	text, ok := mcp.AsTextContent(result.Content[0])
	if !ok {
		t.Fatal("expected TextContent")
	}
	if strings.Contains(text.Text, multiInstanceEnvVar) {
		t.Fatalf("explicit port must clear the multi-instance gate: %q", text.Text)
	}
	if !strings.Contains(text.Text, "Connection to Godot") {
		t.Fatalf("expected a dial failure, got: %q", text.Text)
	}
}

func TestConnectWithoutMultiInstanceModeSkipsPortGate(t *testing.T) {
	t.Setenv(multiInstanceEnvVar, "")
	s := New()

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"auth_token": testMCPAuthToken, "host": "127.0.0.1"}
	result, err := s.handleConnect(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected isError=true because no Godot is listening on the default port")
	}
	text, ok := mcp.AsTextContent(result.Content[0])
	if !ok {
		t.Fatal("expected TextContent")
	}
	if strings.Contains(text.Text, multiInstanceEnvVar) {
		t.Fatalf("single-instance default flow must not require an explicit port: %q", text.Text)
	}
}

func TestMultiInstanceModeEnabled(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"nonsense", false},
		{"1", true},
		{"true", true},
		{"TRUE", true},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Setenv(multiInstanceEnvVar, tt.raw)
			if got := multiInstanceModeEnabled(); got != tt.want {
				t.Fatalf("multiInstanceModeEnabled() with %q = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestFormatConnectSuccessWarnsOnSharedDefaultPort(t *testing.T) {
	shared := formatConnectSuccess("127.0.0.1", defaultSharedPort, "default", `{"ok":true}`)
	if !strings.Contains(shared, "shared") {
		t.Fatalf("connecting on the shared default port must warn: %q", shared)
	}
	if !strings.Contains(shared, "godot_launch") {
		t.Fatalf("shared-port warning must point at the paved road: %q", shared)
	}

	private := formatConnectSuccess("127.0.0.1", defaultSharedPort+1, "default", `{"ok":true}`)
	if strings.Contains(private, "shared") {
		t.Fatalf("a non-default port must not warn about sharing: %q", private)
	}
	if !strings.Contains(private, `{"ok":true}`) {
		t.Fatalf("connect result must include the ping payload: %q", private)
	}
}
