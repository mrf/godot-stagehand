//go:build godot

package mcpserver

import (
	"context"
	"encoding/json"
	"math"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/mrf/godot-stagehand/internal/launch"
)

// TestMCPSetPropertyPreservesJSONTypes is the end-to-end regression test for
// godot-stagehand-set-property-bool-string-coercion-5fz. It deliberately enters
// through MCPServer.HandleMessage, crosses the Go WebSocket client, and reaches
// the real GDScript property handler before reading the value back through MCP.
func TestMCPSetPropertyPreservesJSONTypes(t *testing.T) {
	srv := startMCPServerWithGodot(t)

	tests := []struct {
		name     string
		property string
		value    any
		want     any
		wantType string
	}{
		{
			name:     "native_bool",
			property: "flag_prop",
			value:    false,
			want:     false,
			wantType: "bool",
		},
		{
			name:     "string_bool_from_unconstrained_mcp_client",
			property: "string_bool_prop",
			value:    "false",
			want:     false,
			wantType: "bool",
		},
		{
			name:     "string_property_preserves_false_literal",
			property: "text_prop",
			value:    "false",
			want:     "false",
			wantType: "String",
		},
		{
			name:     "variant_property_preserves_false_literal",
			property: "variant_bool_prop",
			value:    "false",
			want:     "false",
			wantType: "String",
		},
		{
			name:     "vector2",
			property: "vector2_prop",
			value:    map[string]any{"x": 11.5, "y": -2.25},
			want:     map[string]any{"x": 11.5, "y": -2.25},
			wantType: "Vector2",
		},
		{
			name:     "vector3_object",
			property: "vector3_prop",
			value:    map[string]any{"x": 11.7, "y": 7.55, "z": 19.7},
			want:     map[string]any{"x": 11.7, "y": 7.55, "z": 19.7},
			wantType: "Vector3",
		},
		{
			name:     "vector3_array",
			property: "vector3_prop",
			value:    []any{-4.5, 6.25, 9.75},
			want:     map[string]any{"x": -4.5, "y": 6.25, "z": 9.75},
			wantType: "Vector3",
		},
		{
			name:     "vector2i",
			property: "vector2i_prop",
			value:    map[string]any{"x": 8, "y": -3},
			want:     map[string]any{"x": 8, "y": -3},
			wantType: "Vector2i",
		},
		{
			name:     "color",
			property: "color_prop",
			value:    map[string]any{"r": 0.1, "g": 0.2, "b": 0.3, "a": 0.4},
			want:     map[string]any{"r": 0.1, "g": 0.2, "b": 0.3, "a": 0.4},
			wantType: "Color",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setText := callToolThroughMCP(t, srv, "godot_set_property", map[string]any{
				"selector": "/root/TestScene/PropertyTarget",
				"property": tt.property,
				"value":    tt.value,
			})
			var setResult struct {
				Success bool `json:"success"`
			}
			if err := json.Unmarshal([]byte(setText), &setResult); err != nil {
				t.Fatalf("decode set_property result %q: %v", setText, err)
			}
			if !setResult.Success {
				t.Fatalf("set_property returned success=false: %s", setText)
			}

			getText := callToolThroughMCP(t, srv, "godot_get_property", map[string]any{
				"selector": "/root/TestScene/PropertyTarget",
				"property": tt.property,
			})
			var getResult struct {
				Value any    `json:"value"`
				Type  string `json:"type"`
			}
			if err := json.Unmarshal([]byte(getText), &getResult); err != nil {
				t.Fatalf("decode get_property result %q: %v", getText, err)
			}

			want := normalizeJSONValue(t, tt.want)
			if !equalJSONValue(getResult.Value, want) {
				t.Fatalf("read-back value = %#v, want %#v", getResult.Value, want)
			}
			if getResult.Type != tt.wantType {
				t.Fatalf("read-back type = %q, want %q", getResult.Type, tt.wantType)
			}
		})
	}
}

// TestMCPSetPropertyAcceptsStringifiedClientValues is the end-to-end
// regression test for godot-stagehand-set-property-value-stringified-e7er.
//
// TestMCPSetPropertyPreservesJSONTypes above sends native Go values, which
// marshal to native JSON — that is the path a client takes when "value" has a
// declared schema type. The reported failure is the other path: because
// mcp.WithAny left "value" typeless, the reporter's client sent the argument as
// raw JSON *text* ("50", "{\"x\": 11.7, ...}"), which reached GDScript as a
// String. bool and String targets survived that (the addon string-parses bools,
// and a String target takes the text verbatim); every int, float, Vector and
// Color target hard-failed the conversion gate with invalid_value.
//
// Each case here therefore sends a JSON string and asserts the type GDScript
// actually ended up holding — a value-only assertion would pass on a String
// property that merely stored the text.
func TestMCPSetPropertyAcceptsStringifiedClientValues(t *testing.T) {
	srv := startMCPServerWithGodot(t)

	tests := []struct {
		name     string
		property string
		value    string
		want     any
		wantType string
	}{
		{name: "bool_false", property: "flag_prop", value: "false", want: false, wantType: "bool"},
		{name: "bool_true", property: "string_bool_prop", value: "true", want: true, wantType: "bool"},
		{name: "int_zero", property: "count_prop", value: "0", want: float64(0), wantType: "int"},
		{name: "int_nonzero", property: "count_prop", value: "5", want: float64(5), wantType: "int"},
		{name: "int_negative", property: "count_prop", value: "-12", want: float64(-12), wantType: "int"},
		{name: "float", property: "ratio_prop", value: "50", want: float64(50), wantType: "float"},
		{name: "float_fractional", property: "ratio_prop", value: "7.25", want: 7.25, wantType: "float"},
		{
			name:     "string_stays_verbatim",
			property: "text_prop",
			value:    "50",
			want:     "50",
			wantType: "String",
		},
		{
			name:     "string_json_object_stays_verbatim",
			property: "text_prop",
			value:    `{"x": 1}`,
			want:     `{"x": 1}`,
			wantType: "String",
		},
		{
			name:     "vector2_object",
			property: "vector2_prop",
			value:    `{"x": 11.5, "y": -2.25}`,
			want:     map[string]any{"x": 11.5, "y": -2.25},
			wantType: "Vector2",
		},
		{
			name:     "vector3_object",
			property: "vector3_prop",
			value:    `{"x": 11.7, "y": 7.55, "z": 19.7}`,
			want:     map[string]any{"x": 11.7, "y": 7.55, "z": 19.7},
			wantType: "Vector3",
		},
		{
			name:     "vector3_array",
			property: "vector3_prop",
			value:    `[-4.5, 6.25, 9.75]`,
			want:     map[string]any{"x": -4.5, "y": 6.25, "z": 9.75},
			wantType: "Vector3",
		},
		{
			name:     "vector2i_object",
			property: "vector2i_prop",
			value:    `{"x": 8, "y": -3}`,
			want:     map[string]any{"x": 8, "y": -3},
			wantType: "Vector2i",
		},
		{
			name:     "color_object",
			property: "color_prop",
			value:    `{"r": 0.1, "g": 0.2, "b": 0.3, "a": 0.4}`,
			want:     map[string]any{"r": 0.1, "g": 0.2, "b": 0.3, "a": 0.4},
			wantType: "Color",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setText := callToolThroughMCP(t, srv, "godot_set_property", map[string]any{
				"selector": "/root/TestScene/PropertyTarget",
				"property": tt.property,
				"value":    tt.value,
			})
			var setResult struct {
				Success bool `json:"success"`
			}
			if err := json.Unmarshal([]byte(setText), &setResult); err != nil {
				t.Fatalf("decode set_property result %q: %v", setText, err)
			}
			if !setResult.Success {
				t.Fatalf("set_property returned success=false: %s", setText)
			}

			getText := callToolThroughMCP(t, srv, "godot_get_property", map[string]any{
				"selector": "/root/TestScene/PropertyTarget",
				"property": tt.property,
			})
			var getResult struct {
				Value any    `json:"value"`
				Type  string `json:"type"`
			}
			if err := json.Unmarshal([]byte(getText), &getResult); err != nil {
				t.Fatalf("decode get_property result %q: %v", getText, err)
			}

			if getResult.Type != tt.wantType {
				t.Fatalf("read-back type = %q, want %q (value %#v)", getResult.Type, tt.wantType, getResult.Value)
			}
			want := normalizeJSONValue(t, tt.want)
			if !equalJSONValue(getResult.Value, want) {
				t.Fatalf("read-back value = %#v, want %#v", getResult.Value, want)
			}
		})
	}
}

func equalJSONValue(got any, want any) bool {
	switch wantValue := want.(type) {
	case float64:
		gotValue, ok := got.(float64)
		return ok && math.Abs(gotValue-wantValue) <= 0.00001
	case map[string]any:
		gotMap, ok := got.(map[string]any)
		if !ok || len(gotMap) != len(wantValue) {
			return false
		}
		for key, value := range wantValue {
			if !equalJSONValue(gotMap[key], value) {
				return false
			}
		}
		return true
	case []any:
		gotSlice, ok := got.([]any)
		if !ok || len(gotSlice) != len(wantValue) {
			return false
		}
		for index, value := range wantValue {
			if !equalJSONValue(gotSlice[index], value) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(got, want)
	}
}

func startMCPServerWithGodot(t *testing.T) *Server {
	t.Helper()

	godotBin, err := launch.FindGodotBinary()
	if err != nil {
		t.Fatalf("locate Godot: %v", err)
	}
	if godotBin == "" {
		t.Fatal("Godot binary not found; this test requires the 'godot' build tag only in a Godot-equipped environment")
	}

	root := setPropertyRepoRoot(t)
	projectDir := prepareMCPGodotProject(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := launch.Launch(ctx, launch.Config{
		ProjectPath: projectDir,
		GodotBin:    godotBin,
		Host:        "127.0.0.1",
		Port:        freeSetPropertyPort(t),
		Headless:    true,
	})
	if err != nil {
		t.Fatalf("launch Godot: %v", err)
	}
	t.Cleanup(func() {
		_ = result.Conn.Close()
		if err := result.Kill(); err != nil {
			t.Errorf("stop Godot: %v", err)
		}
	})

	srv := New()
	srv.setConn(result.Conn)
	return srv
}

func callToolThroughMCP(t *testing.T, srv *Server, name string, arguments map[string]any) string {
	t.Helper()

	request, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": arguments,
		},
	})
	if err != nil {
		t.Fatalf("encode %s request: %v", name, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	response := srv.mcp.HandleMessage(ctx, request)
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("encode %s response: %v", name, err)
	}

	var envelope struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatalf("decode %s MCP response %s: %v", name, encoded, err)
	}
	if envelope.Error != nil {
		t.Fatalf("%s MCP error %d: %s", name, envelope.Error.Code, envelope.Error.Message)
	}
	if envelope.Result.IsError {
		text := ""
		if len(envelope.Result.Content) > 0 {
			text = envelope.Result.Content[0].Text
		}
		t.Fatalf("%s tool error: %s", name, text)
	}
	if len(envelope.Result.Content) != 1 || envelope.Result.Content[0].Type != "text" {
		t.Fatalf("%s returned unexpected MCP content: %s", name, encoded)
	}
	return envelope.Result.Content[0].Text
}

func normalizeJSONValue(t *testing.T, value any) any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("normalize value: %v", err)
	}
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		t.Fatalf("normalize value: %v", err)
	}
	return normalized
}

func setPropertyRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return root
}

func freeSetPropertyPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release local port: %v", err)
	}
	return port
}
