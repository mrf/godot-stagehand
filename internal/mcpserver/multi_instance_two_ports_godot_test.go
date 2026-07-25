//go:build godot

package mcpserver

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mrf/godot-stagehand/internal/launch"
)

// TestE2E_TwoRealGodotInstancesOnTwoPorts is the real-process counterpart to
// TestE2E_MultiInstance_TwoStubs: instead of two connections to fake stub
// servers, this launches two ACTUAL Godot processes through the real
// godot_launch handler, each on its own auto-assigned port, and proves
// instance_id routing keeps their state independent. Fills M6's "two real
// Godot instances on two ports" gap.
func TestE2E_TwoRealGodotInstancesOnTwoPorts(t *testing.T) {
	godotBin, err := launch.FindGodotBinary()
	if err != nil {
		t.Fatalf("locate Godot: %v", err)
	}
	if godotBin == "" {
		t.Fatal("Godot binary not found; this test requires the 'godot' build tag only in a Godot-equipped environment")
	}
	root := setPropertyRepoRoot(t)

	srv := New()
	t.Cleanup(srv.instances.closeAll)

	launchInstance := func(instanceID string) {
		t.Helper()
		projectDir := prepareMCPGodotProject(t, root)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		result, err := srv.handleLaunch(ctx, toolReq(map[string]any{
			"project_path": projectDir,
			"godot_bin":    godotBin,
			"headless":     true,
			"instance_id":  instanceID,
		}))
		if err != nil {
			t.Fatalf("handleLaunch(%q): %v", instanceID, err)
		}
		if result.IsError {
			t.Fatalf("handleLaunch(%q) failed: %s", instanceID, mustText(t, result))
		}
	}
	launchInstance("p1")
	launchInstance("p2")

	e1 := srv.instances.get("p1")
	e2 := srv.instances.get("p2")
	if e1 == nil || e2 == nil {
		t.Fatal("expected both real instances to be registered")
	}
	if e1.port == e2.port {
		t.Fatalf("expected two real Godot instances on distinct ports, both got %d", e1.port)
	}
	if e1.pid == e2.pid {
		t.Fatalf("expected two distinct real Godot process ids, both got %d", e1.pid)
	}

	ctx := context.Background()
	for _, id := range []string{"p1", "p2"} {
		r, err := srv.handleGetTree(ctx, toolReq(map[string]any{"instance_id": id}))
		if err != nil {
			t.Fatalf("get_tree(%s): %v", id, err)
		}
		if r.IsError {
			t.Fatalf("get_tree(%s) failed: %s", id, mustText(t, r))
		}
	}

	// flag_prop defaults to true (testdata/test_project/scripts/property_target.gd).
	// Flip it on p1 only; p2 must be unaffected if the two live instances are
	// really isolated.
	setR, err := srv.handleSetProperty(ctx, toolReq(map[string]any{
		"instance_id": "p1",
		"selector":    "/root/TestScene/PropertyTarget",
		"property":    "flag_prop",
		"value":       false,
	}))
	if err != nil {
		t.Fatalf("set_property on p1: %v", err)
	}
	if setR.IsError {
		t.Fatalf("set_property on p1 failed: %s", mustText(t, setR))
	}

	if got := getFlagProp(t, srv, "p1"); got != false {
		t.Errorf("p1 flag_prop = %v, want false after set_property", got)
	}
	if got := getFlagProp(t, srv, "p2"); got != true {
		t.Errorf("p2 flag_prop = %v, want true (untouched default) -- cross-instance leak if false", got)
	}
}

func getFlagProp(t *testing.T, srv *Server, instanceID string) bool {
	t.Helper()
	r, err := srv.handleGetProperty(context.Background(), toolReq(map[string]any{
		"instance_id": instanceID,
		"selector":    "/root/TestScene/PropertyTarget",
		"property":    "flag_prop",
	}))
	if err != nil {
		t.Fatalf("get_property(%s): %v", instanceID, err)
	}
	if r.IsError {
		t.Fatalf("get_property(%s) failed: %s", instanceID, mustText(t, r))
	}
	var result struct {
		Value bool `json:"value"`
	}
	if err := json.Unmarshal([]byte(mustText(t, r)), &result); err != nil {
		t.Fatalf("decode get_property(%s): %v", instanceID, err)
	}
	return result.Value
}
