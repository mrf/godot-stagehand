package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// setupNamedInstance connects a new stub Godot server and connects the given
// Server to it under the supplied instanceID.
func setupNamedInstance(t *testing.T, srv *Server, instanceID string) *stubGodot {
	t.Helper()
	stub := newStubGodot(t)
	t.Cleanup(func() { stub.Close() })

	host, port := serverHostPort(t, stub.Server)
	ctx := context.Background()

	result, err := srv.handleConnect(ctx, toolReq(map[string]any{
		"host":        host,
		"port":        float64(port),
		"instance_id": instanceID,
	}))
	if err != nil {
		t.Fatalf("setupNamedInstance(%q): handleConnect error: %v", instanceID, err)
	}
	if result.IsError {
		t.Fatalf("setupNamedInstance(%q): connection failed: %+v", instanceID, result)
	}
	return stub
}

func TestE2E_MultiInstance_TwoStubs(t *testing.T) {
	srv := New()

	stub1 := setupNamedInstance(t, srv, "player1")
	stub2 := setupNamedInstance(t, srv, "player2")

	ctx := context.Background()

	// Send get_tree to player1.
	r1, err := srv.handleGetTree(ctx, toolReq(map[string]any{
		"instance_id": "player1",
	}))
	if err != nil {
		t.Fatalf("player1 get_tree: %v", err)
	}
	if r1.IsError {
		t.Fatalf("player1 get_tree error: %+v", r1)
	}

	// Send get_tree to player2.
	r2, err := srv.handleGetTree(ctx, toolReq(map[string]any{
		"instance_id": "player2",
	}))
	if err != nil {
		t.Fatalf("player2 get_tree: %v", err)
	}
	if r2.IsError {
		t.Fatalf("player2 get_tree error: %+v", r2)
	}

	// Both stubs must have received exactly one get_tree call each.
	if n := stub1.callCount("get_tree"); n != 1 {
		t.Errorf("stub1 expected 1 get_tree call, got %d", n)
	}
	if n := stub2.callCount("get_tree"); n != 1 {
		t.Errorf("stub2 expected 1 get_tree call, got %d", n)
	}
}

func TestE2E_MultiInstance_DefaultBackwardCompat(t *testing.T) {
	srv, _ := setupE2ETest(t)
	ctx := context.Background()

	// A call without instance_id should route to "default" and work fine.
	result, err := srv.handleGetTree(ctx, toolReq(map[string]any{}))
	if err != nil {
		t.Fatalf("default get_tree: %v", err)
	}
	if result.IsError {
		t.Fatalf("default get_tree error: %+v", result)
	}
}

func TestE2E_MultiInstance_CommandsToEachInstance(t *testing.T) {
	srv := New()
	stub1 := setupNamedInstance(t, srv, "p1")
	stub2 := setupNamedInstance(t, srv, "p2")
	ctx := context.Background()

	// Send click to p1.
	_, err := srv.handleClick(ctx, toolReq(map[string]any{
		"instance_id": "p1",
		"selector":    "class:Button",
	}))
	if err != nil {
		t.Fatalf("p1 click: %v", err)
	}

	// Send press_key to p2.
	_, err = srv.handlePressKey(ctx, toolReq(map[string]any{
		"instance_id": "p2",
		"key":         "Space",
	}))
	if err != nil {
		t.Fatalf("p2 press_key: %v", err)
	}

	if n := stub1.callCount("input_mouse"); n != 1 {
		t.Errorf("p1: expected 1 input_mouse, got %d", n)
	}
	if n := stub1.callCount("input_key"); n != 0 {
		t.Errorf("p1: expected 0 input_key calls (key went to wrong instance), got %d", n)
	}
	if n := stub2.callCount("input_key"); n != 1 {
		t.Errorf("p2: expected 1 input_key, got %d", n)
	}
	if n := stub2.callCount("input_mouse"); n != 0 {
		t.Errorf("p2: expected 0 input_mouse calls (click went to wrong instance), got %d", n)
	}
}

func TestE2E_ListInstances_Empty(t *testing.T) {
	srv := New()
	ctx := context.Background()

	result, err := srv.handleListInstances(ctx, toolReq(nil))
	if err != nil {
		t.Fatalf("list_instances: %v", err)
	}
	if result.IsError {
		t.Fatalf("list_instances error: %+v", result)
	}
	text := mustText(t, result)
	if !strings.Contains(text, `"instances"`) {
		t.Errorf("expected instances key, got: %s", text)
	}

	var out struct {
		Instances []any `json:"instances"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal list_instances: %v", err)
	}
	if len(out.Instances) != 0 {
		t.Errorf("expected 0 instances, got %d", len(out.Instances))
	}
}

func TestE2E_ListInstances_ShowsConnected(t *testing.T) {
	srv := New()
	setupNamedInstance(t, srv, "default")
	setupNamedInstance(t, srv, "extra")
	ctx := context.Background()

	result, err := srv.handleListInstances(ctx, toolReq(nil))
	if err != nil {
		t.Fatalf("list_instances: %v", err)
	}
	if result.IsError {
		t.Fatalf("list_instances error: %+v", result)
	}
	text := mustText(t, result)

	var out struct {
		Instances []struct {
			ID        string `json:"id"`
			Host      string `json:"host"`
			Port      int    `json:"port"`
			PID       int    `json:"pid"`
			Connected bool   `json:"connected"`
		} `json:"instances"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal list_instances: %v", err)
	}
	if len(out.Instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(out.Instances))
	}

	// Sorted by ID: "default" < "extra".
	if out.Instances[0].ID != "default" {
		t.Errorf("first instance id = %q, want %q", out.Instances[0].ID, "default")
	}
	if out.Instances[1].ID != "extra" {
		t.Errorf("second instance id = %q, want %q", out.Instances[1].ID, "extra")
	}
	for _, inst := range out.Instances {
		if !inst.Connected {
			t.Errorf("instance %q: expected connected=true", inst.ID)
		}
		if inst.Host == "" {
			t.Errorf("instance %q: expected non-empty host", inst.ID)
		}
		if inst.Port == 0 {
			t.Errorf("instance %q: expected non-zero port", inst.ID)
		}
	}
}

func TestE2E_Disconnect_Existing(t *testing.T) {
	srv := New()
	setupNamedInstance(t, srv, "toremove")
	ctx := context.Background()

	result, err := srv.handleDisconnect(ctx, toolReq(map[string]any{
		"instance_id": "toremove",
	}))
	if err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if result.IsError {
		t.Fatalf("disconnect error: %+v", result)
	}
	text := mustText(t, result)
	if !strings.Contains(text, `"disconnected":true`) {
		t.Errorf("expected disconnected:true, got: %s", text)
	}
	if !strings.Contains(text, `"toremove"`) {
		t.Errorf("expected instance_id in result, got: %s", text)
	}

	// Subsequent commands to that instance should fail.
	r2, err := srv.handleGetTree(ctx, toolReq(map[string]any{
		"instance_id": "toremove",
	}))
	if err != nil {
		t.Fatalf("get_tree after disconnect: %v", err)
	}
	if !r2.IsError {
		t.Error("expected error after disconnecting instance")
	}
}

func TestE2E_Disconnect_NotFound(t *testing.T) {
	srv := New()
	ctx := context.Background()

	result, err := srv.handleDisconnect(ctx, toolReq(map[string]any{
		"instance_id": "nobody",
	}))
	if err != nil {
		t.Fatalf("disconnect not found: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when disconnecting non-existent instance")
	}
}

func TestE2E_UnknownInstance_ReturnsError(t *testing.T) {
	srv := New()
	ctx := context.Background()

	result, err := srv.handleGetTree(ctx, toolReq(map[string]any{
		"instance_id": "no-such-instance",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for unknown instance_id")
	}
	text := mustText(t, result)
	if !strings.Contains(text, "no-such-instance") {
		t.Errorf("error should mention the instance id, got: %s", text)
	}
}

func TestE2E_MultiInstance_ToolsRouteCorrectly(t *testing.T) {
	// Verify every tool that accepts instance_id routes to the right stub.
	srv := New()
	stub1 := setupNamedInstance(t, srv, "inst1")
	stub2 := setupNamedInstance(t, srv, "inst2")
	_ = stub2
	ctx := context.Background()

	// game_state to inst1 only.
	_, _ = srv.handleGetGameState(ctx, toolReq(map[string]any{"instance_id": "inst1"}))
	if n := stub1.callCount("get_game_state"); n != 1 {
		t.Errorf("get_game_state: stub1 got %d, want 1", n)
	}
	if n := stub2.callCount("get_game_state"); n != 0 {
		t.Errorf("get_game_state: stub2 got %d, want 0", n)
	}
}
