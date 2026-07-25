//go:build linux

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

// reexecRoleEnv and pidFileEnv drive the same self-reexec pattern os/exec's
// own tests use (TestHelperProcess / GO_WANT_HELPER_PROCESS): the compiled
// test binary re-invokes itself with a role env var so we can exercise real
// process trees without depending on any external binary being on PATH.
const (
	reexecRoleEnv = "STAGEHAND_TEST_PDEATHSIG_ROLE"
	pidFileEnv    = "STAGEHAND_TEST_PDEATHSIG_PIDFILE"
)

func TestMain(m *testing.M) {
	switch os.Getenv(reexecRoleEnv) {
	case "parent":
		runPdeathsigTestParent()
		return
	case "child":
		time.Sleep(30 * time.Second)
		return
	}
	os.Exit(m.Run())
}

// runPdeathsigTestParent stands in for the MCP server: it launches a child
// with sysProcAttr() applied, exactly like Launch() does, reports the child's
// PID, then sleeps so the test can hard-kill this process out from under it.
func runPdeathsigTestParent() {
	self, err := os.Executable()
	if err != nil {
		os.Exit(1)
	}
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(), reexecRoleEnv+"=child")
	cmd.SysProcAttr = sysProcAttr()
	if err := cmd.Start(); err != nil {
		os.Exit(1)
	}
	pidFile := os.Getenv(pidFileEnv)
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		os.Exit(1)
	}
	time.Sleep(30 * time.Second)
}

func TestSysProcAttrLinuxFields(t *testing.T) {
	attr := sysProcAttr()
	if !attr.Setpgid {
		t.Error("Setpgid = false, want true")
	}
	if attr.Pdeathsig != syscall.SIGKILL {
		t.Errorf("Pdeathsig = %v, want SIGKILL", attr.Pdeathsig)
	}
}

// TestHardKillDoesNotLeakChildProcess reproduces the S4 leak: a process tree
// shaped like MCP-server -> launched Godot, with the MCP server hard-killed
// (SIGKILL, simulating a crash/OOM) instead of shutting down gracefully. With
// sysProcAttr() applied, Pdeathsig must take the child down with it.
func TestHardKillDoesNotLeakChildProcess(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	pidFile := t.TempDir() + "/child.pid"

	parent := exec.Command(self)
	parent.Env = append(os.Environ(), reexecRoleEnv+"=parent", pidFileEnv+"="+pidFile)
	if err := parent.Start(); err != nil {
		t.Fatalf("start parent helper: %v", err)
	}

	var childPID int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, readErr := os.ReadFile(pidFile)
		if readErr == nil && len(data) > 0 {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr == nil {
				childPID = pid
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if childPID == 0 {
		_ = parent.Process.Kill()
		t.Fatal("child process never reported its PID")
	}

	// Simulate a hard kill of the MCP server (SIGKILL/panic/OOM), not the
	// graceful shutdown path.
	if err := parent.Process.Kill(); err != nil {
		t.Fatalf("hard-kill parent helper: %v", err)
	}
	_ = parent.Wait()

	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if killErr := syscall.Kill(childPID, 0); killErr != nil {
			return // child is gone: Pdeathsig delivered SIGKILL as expected.
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(childPID, syscall.SIGKILL) // don't leak the stray test process
	t.Fatalf("child pid %d still alive after its parent was hard-killed; Pdeathsig did not fire", childPID)
}
