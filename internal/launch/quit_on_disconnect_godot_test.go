//go:build godot

package launch

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
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
//
// Launch()'s own connection is the first one this instance ever had, so this
// disconnect is the first-ever arm-and-resolve cycle and uses the longer
// INITIAL_QUIT_ON_DISCONNECT_GRACE_MS (30s), not the standard 10s — see
// TestQuitOnDisconnectSecondDisconnectUsesStandardGrace for that path.
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

	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		if killErr := syscall.Kill(pid, 0); killErr != nil {
			return // process is gone: the addon self-quit as expected.
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = result.Kill()
	t.Fatalf("Godot pid %d still alive %s after the last client disconnected; self-quit did not fire", pid, 35*time.Second)
}

// TestQuitOnDisconnectSecondDisconnectUsesStandardGrace covers the case the
// previous test can't: a disconnect that is NOT the first one this instance
// has ever seen. After one reconnect has already happened, later disconnects
// must fall back to the standard, shorter QUIT_ON_DISCONNECT_GRACE_MS (10s)
// rather than the longer initial grace — the initial grace exists to absorb
// a slow first connection (or an incidental TCP probe), not to make every
// abandoned-launch cleanup take 30s.
func TestQuitOnDisconnectSecondDisconnectUsesStandardGrace(t *testing.T) {
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
	pid := result.Process.Process.Pid
	defer func() {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}()

	// First disconnect (the initial-grace path, covered above).
	if err := result.Conn.Close(); err != nil {
		t.Fatalf("close first connection: %v", err)
	}

	reconnectCtx, reconnectCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer reconnectCancel()
	conn2, err := dialGodotWhenReady(reconnectCtx, "127.0.0.1", port, nil)
	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}

	// Second disconnect: must use the standard 10s grace, not the 30s initial one.
	if err := conn2.Close(); err != nil {
		t.Fatalf("close second connection: %v", err)
	}

	deadline := time.Now().Add(13 * time.Second)
	for time.Now().Before(deadline) {
		if killErr := syscall.Kill(pid, 0); killErr != nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("Godot pid %d still alive %s after the SECOND disconnect; standard grace did not fire", pid, 13*time.Second)
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

// TestQuitOnDisconnectNeverConnectedInstanceSurvives launches a real Godot
// instance and never connects to it at all — not a WebSocket session, not
// even a raw TCP dial — then confirms it is still alive well past the
// standard QUIT_ON_DISCONNECT_GRACE_MS (10s). This locks down what
// instrumentation confirmed for godot-stagehand-mt3i: a genuinely
// never-touched port never arms the quit timer, because `_had_client`
// (stagehand_server.gd) is never set without some connection attempt. It
// also confirms the new hand-rolled-launch startup message (no
// STAGEHAND_INSTANCE_TOKEN set, matching this launch) appears up front.
func TestQuitOnDisconnectNeverConnectedInstanceSurvives(t *testing.T) {
	godotBin := requireGodotBinary(t)
	projectDir := prepareGodotTestProject(t, findProjectRoot(t))
	port := freeTCPPort(t)

	cmd, out := spawnGodotHeadlessWithoutConnecting(t, godotBin, projectDir, port)
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	waitForLogLine(t, out, "Stagehand: Server listening", godotStartupTimeout)

	if !strings.Contains(out.String(), "STAGEHAND_QUIT_ON_DISCONNECT=0") {
		t.Fatalf("expected a startup message naming the opt-out for a hand-rolled launch; log:\n%s", out.String())
	}

	time.Sleep(13 * time.Second)

	if err := syscall.Kill(cmd.Process.Pid, 0); err != nil {
		t.Fatalf("process exited without any client ever connecting: %v (log:\n%s)", err, out.String())
	}
}

// TestQuitOnDisconnectSurvivesBareTCPProbeWithinInitialGrace reproduces the
// actual mechanism confirmed via instrumentation for godot-stagehand-mt3i: a
// bare TCP connect-and-close (no WebSocket upgrade at all — e.g. a
// port-liveness probe) is enough to set `_had_client`, and once that
// half-open peer is reaped the quit timer arms. Before this fix that alone
// used the short QUIT_ON_DISCONNECT_GRACE_MS (10s) and would kill the
// process before a real client — on a human/agent's slower clock — ever got
// a chance to connect. This asserts the longer initial grace covers it, and
// that a real client connecting afterward still succeeds.
func TestQuitOnDisconnectSurvivesBareTCPProbeWithinInitialGrace(t *testing.T) {
	godotBin := requireGodotBinary(t)
	projectDir := prepareGodotTestProject(t, findProjectRoot(t))
	port := freeTCPPort(t)

	cmd, out := spawnGodotHeadlessWithoutConnecting(t, godotBin, projectDir, port)
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	waitForLogLine(t, out, "Stagehand: Server listening", godotStartupTimeout)

	probeConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("probe dial: %v", err)
	}
	probeConn.Close()

	// Outlast the standard grace (10s); pre-fix this alone killed the process.
	time.Sleep(12 * time.Second)
	if err := syscall.Kill(cmd.Process.Pid, 0); err != nil {
		t.Fatalf("process quit within the initial grace after only a bare TCP probe: %v (log:\n%s)", err, out.String())
	}

	conn, err := dialGodotWhenReady(context.Background(), "127.0.0.1", port, nil)
	if err != nil {
		t.Fatalf("real client failed to connect within the initial grace: %v (log:\n%s)", err, out.String())
	}
	conn.Close()
}

// spawnGodotHeadlessWithoutConnecting starts a headless Godot process with
// the stagehand addon enabled but never establishes any connection to it,
// so tests can observe the quit-on-disconnect guard against an instance
// that genuinely never had a client. STAGEHAND_INSTANCE_TOKEN is
// deliberately left unset: only internal/launch.Launch sets it, so its
// absence mirrors a hand-rolled/manual launch like the one that reported
// godot-stagehand-mt3i.
func spawnGodotHeadlessWithoutConnecting(t *testing.T, godotBin, projectDir string, port int) (*exec.Cmd, *syncBuffer) {
	t.Helper()
	cmd := exec.Command(godotBin, "--headless", "--path", projectDir, "--", "--stagehand")
	cmd.Env = append(os.Environ(),
		"STAGEHAND_ENABLED=1",
		fmt.Sprintf("STAGEHAND_PORT=%d", port),
	)
	out := &syncBuffer{}
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		t.Fatalf("start Godot: %v", err)
	}
	return cmd, out
}

// syncBuffer is a concurrency-safe bytes.Buffer for capturing a subprocess's
// combined stdout/stderr while a test polls its contents from another
// goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// waitForLogLine polls out until it contains substr or timeout elapses.
func waitForLogLine(t *testing.T, out *syncBuffer, substr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), substr) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for log line %q; log so far:\n%s", substr, out.String())
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
