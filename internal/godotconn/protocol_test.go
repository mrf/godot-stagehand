package godotconn

import (
	"encoding/json"
	"testing"
)

func TestRequestMarshal(t *testing.T) {
	req := newRequest(42, "ping", nil)
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v, want 2.0", got["jsonrpc"])
	}
	if got["id"] != float64(42) {
		t.Errorf("id = %v, want 42", got["id"])
	}
	if got["method"] != "ping" {
		t.Errorf("method = %v, want ping", got["method"])
	}
	if _, ok := got["params"]; ok {
		t.Error("params should be omitted when nil")
	}
}

func TestRequestMarshalWithParams(t *testing.T) {
	params := map[string]any{"selector": "class:Button"}
	req := newRequest(1, "query_nodes", params)
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	var got Request
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Method != "query_nodes" {
		t.Errorf("method = %q, want query_nodes", got.Method)
	}
}

func TestResponseUnmarshalResult(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"result":{"nodes":[]}}`
	var resp Response
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != 1 {
		t.Errorf("id = %d, want 1", resp.ID)
	}
	if resp.Error != nil {
		t.Error("error should be nil for success response")
	}
	if resp.Result == nil {
		t.Error("result should not be nil")
	}
}

func TestResponseUnmarshalError(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"method not found"}}`
	var resp Response
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != 2 {
		t.Errorf("id = %d, want 2", resp.ID)
	}
	if resp.Error == nil {
		t.Fatal("error should not be nil")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("code = %d, want %d", resp.Error.Code, CodeMethodNotFound)
	}
	if resp.Error.Error() != "method not found" {
		t.Errorf("error message = %q", resp.Error.Error())
	}
}

func TestRPCErrorImplementsError(t *testing.T) {
	var err error = &RPCError{Code: CodeInternalError, Message: "boom"}
	if err.Error() != "boom" {
		t.Errorf("Error() = %q, want boom", err.Error())
	}
}
