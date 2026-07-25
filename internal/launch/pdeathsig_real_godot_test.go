//go:build linux && godot

package launch

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestHardKillDoesNotLeakRealGodotProcess is the real-binary version of
// TestHardKillDoesNotLeakChildProcess: the "child" held down by Pdeathsig is
// an actual launched Godot process (via Launch(), the same code path the MCP
// server uses), not a synthetic sleeping stand-in. Fills M6's "process leak
// on crash" gap for the real multi-instance scenario: a hard-killed MCP
// server (SIGKILL/panic/OOM) must not leave a live Godot process holding its
// port.
func TestHardKillDoesNotLeakRealGodotProcess(t *testing.T) {
	godotBin := requireGodotBinary(t)
	projectDir := prepareGodotTestProject(t, findProjectRoot(t))
	port := freeTCPPort(t)

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	pidFile := t.TempDir() + "/godot.pid"

	parent := exec.Command(self)
	parent.Env = append(os.Environ(),
		reexecRoleEnv+"=godot-parent",
		pidFileEnv+"="+pidFile,
		projectEnv+"="+projectDir,
		godotBinEnv+"="+godotBin,
		portEnv+"="+strconv.Itoa(port),
	)
	parent.Stdout = os.Stderr
	parent.Stderr = os.Stderr
	if err := parent.Start(); err != nil {
		t.Fatalf("start godot-parent helper: %v", err)
	}

	var godotPID int
	deadline := time.Now().Add(godotStartupTimeout + 15*time.Second)
	for time.Now().Before(deadline) {
		data, readErr := os.ReadFile(pidFile)
		if readErr == nil && len(data) > 0 {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr == nil {
				godotPID = pid
				break
			}
		}
		if parent.ProcessState != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if godotPID == 0 {
		_ = parent.Process.Kill()
		_ = parent.Wait()
		t.Fatal("godot-parent helper never reported the launched Godot pid; see stderr above for the launch failure")
	}

	// Simulate a hard kill of the MCP server (SIGKILL/panic/OOM), not the
	// graceful shutdown path that calls LaunchResult.Kill().
	if err := parent.Process.Kill(); err != nil {
		t.Fatalf("hard-kill godot-parent helper: %v", err)
	}
	_ = parent.Wait()

	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if killErr := syscall.Kill(godotPID, 0); killErr != nil {
			return // the real Godot process is gone: Pdeathsig delivered SIGKILL as expected.
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(godotPID, syscall.SIGKILL) // don't leak the stray Godot process
	t.Fatalf("real Godot pid %d still alive after its parent was hard-killed; Pdeathsig did not fire", godotPID)
}
