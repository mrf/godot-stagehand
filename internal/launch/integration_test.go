//go:build integration
// +build integration

package launch

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestIntegrationLaunchHeadless(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Godot integration test in short mode")
	}

	godotBin, err := FindGodotBinary()
	if err != nil {
		t.Fatal(err)
	}
	if godotBin == "" {
		t.Skip("Godot binary not found; set GODOT_BIN, GODOT_PATH, or STAGEHAND_GODOT_BIN, or put godot/godot4 in PATH")
	}

	projectRoot := findProjectRoot(t)
	projectDir := prepareGodotTestProject(t, projectRoot)
	port := freeTCPPort(t)

	cfg := Config{
		ProjectPath: projectDir,
		GodotBin:    godotBin,
		Host:        "127.0.0.1",
		Port:        port,
		Headless:    true,
		TimeoutMs:   int(godotStartupTimeout.Milliseconds()),
	}

	ctx := context.Background()
	result, err := Launch(ctx, cfg)
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}
	defer result.Kill()
	defer result.Conn.Close()

	// Verify connection via ping.
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	pingResp, err := result.Conn.Call(pingCtx, "ping", nil)
	if err != nil {
		t.Fatalf("ping call failed: %v", err)
	}
	if pingResp.Error != nil {
		t.Fatalf("ping returned error: %v", pingResp.Error)
	}
	var ping struct {
		Status           string `json:"status"`
		Engine           string `json:"engine"`
		EngineVersion    string `json:"engine_version"`
		StagehandVersion string `json:"stagehand_version"`
		InstanceToken    string `json:"instance_token"`
	}
	if err := json.Unmarshal(pingResp.Result, &ping); err != nil {
		t.Fatalf("unmarshal ping result: %v; raw=%s", err, pingResp.Result)
	}
	if ping.Status != "ok" || ping.Engine != "godot" {
		t.Fatalf("unexpected ping response: %+v", ping)
	}
	// The real addon must echo a non-empty instance token; a successful Launch
	// already proves it matched the one we passed via env, so any failure here
	// would have been an error from Launch above.
	if ping.InstanceToken == "" {
		t.Errorf("ping response missing instance_token: %+v", ping)
	}
	if ping.EngineVersion == "" || ping.StagehandVersion == "" {
		t.Fatalf("ping response missing version fields: %+v", ping)
	}
	// Ensure launch result fields match.
	if result.EngineVersion != ping.EngineVersion {
		t.Errorf("EngineVersion mismatch: got %q, want %q", result.EngineVersion, ping.EngineVersion)
	}
	if result.StagehandVersion != ping.StagehandVersion {
		t.Errorf("StagehandVersion mismatch: got %q, want %q", result.StagehandVersion, ping.StagehandVersion)
	}
	if result.Port != port {
		t.Errorf("Port mismatch: got %d, want %d", result.Port, port)
	}
	if result.Host != "127.0.0.1" {
		t.Errorf("Host mismatch: got %q, want %q", result.Host, "127.0.0.1")
	}
	// Process should still be running.
	if result.Process.Process == nil {
		t.Fatal("Process missing")
	}
}
