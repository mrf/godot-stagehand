package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestE2E_GetAccessibilityTree exercises the happy path: the tool forwards a
// get_accessibility_tree GWP call and returns the semantic tree verbatim.
func TestE2E_GetAccessibilityTree(t *testing.T) {
	srv, stub := setupE2ETest(t)

	req := mcp.CallToolRequest{}
	req.Params.Name = "godot_get_accessibility_tree"
	req.Params.Arguments = map[string]any{}

	result, err := srv.handleGetAccessibilityTree(context.Background(), req)
	if err != nil {
		t.Fatalf("handleGetAccessibilityTree error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %+v", result)
	}

	text := mustText(t, result)
	for _, want := range []string{`"role"`, `"button"`, `"StartButton"`, `"derived"`} {
		if !strings.Contains(text, want) {
			t.Errorf("accessibility tree result missing %s: %s", want, text)
		}
	}

	if n := stub.callCount("get_accessibility_tree"); n != 1 {
		t.Fatalf("get_accessibility_tree call count = %d, want 1", n)
	}
}

// Defaults must be forwarded explicitly so the addon never has to guess.
func TestE2E_GetAccessibilityTreeDefaults(t *testing.T) {
	srv, stub := setupE2ETest(t)

	req := mcp.CallToolRequest{}
	req.Params.Name = "godot_get_accessibility_tree"
	req.Params.Arguments = map[string]any{}

	if _, err := srv.handleGetAccessibilityTree(context.Background(), req); err != nil {
		t.Fatalf("handleGetAccessibilityTree error: %v", err)
	}

	var params map[string]any
	if err := json.Unmarshal(stub.lastCallParams("get_accessibility_tree"), &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if got := params["root_path"]; got != "/root" {
		t.Errorf("root_path = %v, want /root", got)
	}
	if got := params["max_depth"]; got != float64(10) {
		t.Errorf("max_depth = %v, want 10", got)
	}
}

func TestE2E_GetAccessibilityTreeCustomArgs(t *testing.T) {
	srv, stub := setupE2ETest(t)

	req := mcp.CallToolRequest{}
	req.Params.Name = "godot_get_accessibility_tree"
	req.Params.Arguments = map[string]any{
		"root_path": "/root/UI",
		"max_depth": float64(3),
	}

	if _, err := srv.handleGetAccessibilityTree(context.Background(), req); err != nil {
		t.Fatalf("handleGetAccessibilityTree error: %v", err)
	}

	var params map[string]any
	if err := json.Unmarshal(stub.lastCallParams("get_accessibility_tree"), &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if got := params["root_path"]; got != "/root/UI" {
		t.Errorf("root_path = %v, want /root/UI", got)
	}
	if got := params["max_depth"]; got != float64(3) {
		t.Errorf("max_depth = %v, want 3", got)
	}
}

// The "not available on this Godot version" path must surface as a clean tool
// error, not a panic or an empty success. This is the < 4.5 fallback contract.
func TestE2E_GetAccessibilityTreeUnsupportedVersion(t *testing.T) {
	srv, stub := setupE2ETest(t)
	stub.setAccessibilityUnsupported(true)

	req := mcp.CallToolRequest{}
	req.Params.Name = "godot_get_accessibility_tree"
	req.Params.Arguments = map[string]any{}

	result, err := srv.handleGetAccessibilityTree(context.Background(), req)
	if err != nil {
		t.Fatalf("handleGetAccessibilityTree returned a transport error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected an error result on an unsupported Godot version, got: %+v", result)
	}
	text := mustText(t, result)
	if !strings.Contains(strings.ToLower(text), "4.5") {
		t.Errorf("unsupported-version error should name the required version, got: %s", text)
	}
}

// role: must be accepted by the shared selector validator used by every
// selector-taking tool, so it works in find_nodes/click/etc. too.
func TestRoleSelectorPassesValidation(t *testing.T) {
	for _, sel := range []string{"role:button", "role:check_box", "name:Dialog >> role:button"} {
		if errResult := validateSelector(sel); errResult != nil {
			t.Errorf("validateSelector(%q) rejected a valid role selector: %+v", sel, errResult)
		}
	}
	if errResult := validateSelector("role:"); errResult == nil {
		t.Error("validateSelector(\"role:\") should reject an empty role value")
	}
}

func TestAccessibilityToolIsRegistered(t *testing.T) {
	s := New()
	if _, ok := s.mcp.ListTools()["godot_get_accessibility_tree"]; !ok {
		t.Error("godot_get_accessibility_tree is not registered on the server")
	}
}
