//go:build godot

package launch

import (
	"context"
	"syscall"
	"testing"
	"time"
)

// TestQuitOnDisconnectSelfQuitsAfterGracePeriod launches a real Godot instance,
// closes only the WebSocket connection (not the process), and asserts the
// addon self-quits once no client reconnects within its grace period. This
// covers the "abandoned launch" leak: an MCP server that exits without
// calling Kill() would otherwise leave a ~500MB headless Godot process
// holding its port forever.
func TestQuitOnDisconnectSelfQuitsAfterGracePeriod(t *testing.T) {
	godotBin := requireGodotBinary(t)
	projectDir := prepareGodotTestProject(t, findProjectRoot(t))
	ctx, cancel := context.WithTimeout(context.Background(), godotStartupTimeout)
	defer cancel()
	result, err := Launch(ctx, Config{
		ProjectPath: projectDir,
		GodotBin:    godotBin,
		Host:        "127.0.0.1",
		Port:        freeTCPPort(t),
		Headless:    true,
		TimeoutMs:   int(godotStartupTimeout.Milliseconds()),
	})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	pid := result.Process.Process.Pid

	// Close only the client-side WebSocket; leave the Godot process running,
	// as if the MCP server exited without calling Kill().
	if err := result.Conn.Close(); err != nil {
		t.Fatalf("close connection: %v", err)
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if killErr := syscall.Kill(pid, 0); killErr != nil {
			return // process is gone: the addon self-quit as expected.
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = result.Kill()
	t.Fatalf("Godot pid %d still alive %s after the last client disconnected; self-quit did not fire", pid, 20*time.Second)
}

// TestQuitOnDisconnectOptOutKeepsRunning verifies STAGEHAND_QUIT_ON_DISCONNECT=0
// disables the self-quit: the process must accept a fresh reconnect well past
// what would otherwise be the grace window.
func TestQuitOnDisconnectOptOutKeepsRunning(t *testing.T) {
	t.Setenv("STAGEHAND_QUIT_ON_DISCONNECT", "0")
	godotBin := requireGodotBinary(t)
	projectDir := prepareGodotTestProject(t, findProjectRoot(t))
	port := freeTCPPort(t)
	ctx, cancel := context.WithTimeout(context.Background(), godotStartupTimeout)
	defer cancel()
	result, err := Launch(ctx, Config{
		ProjectPath: projectDir,
		GodotBin:    godotBin,
		Host:        "127.0.0.1",
		Port:        port,
		Headless:    true,
		TimeoutMs:   int(godotStartupTimeout.Milliseconds()),
	})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer result.Kill()

	if err := result.Conn.Close(); err != nil {
		t.Fatalf("close connection: %v", err)
	}

	// Outlast the default 10s grace period the opt-out is meant to disable.
	time.Sleep(12 * time.Second)

	reconnected, err := dialGodotWhenReady(ctx, "127.0.0.1", port, nil)
	if err != nil {
		t.Fatalf("reconnect after opt-out: process should still be running: %v", err)
	}
	defer reconnected.Close()
}

func requireGodotBinary(t *testing.T) string {
	t.Helper()
	godotBin, err := FindGodotBinary()
	if err != nil {
		t.Fatalf("find Godot: %v", err)
	}
	if godotBin == "" {
		t.Fatal("Godot binary not found; the godot build tag requires a Godot-equipped environment")
	}
	return godotBin
}
