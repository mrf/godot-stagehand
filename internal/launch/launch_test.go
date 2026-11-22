package launch

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestGodotArgsActivationFlag(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want []string
	}{
		{"default passes --stagehand", Config{ProjectPath: "p", Headless: true},
			[]string{"--headless", "--path", "p", "--", "--stagehand"}},
		{"extra args follow the flag", Config{ProjectPath: "p", ExtraArgs: []string{"--x"}},
			[]string{"--path", "p", "--", "--stagehand", "--x"}},
		// An own-cli-parser host rejects any user arg it does not know, so the
		// env-only path must not even emit the bare "--".
		{"env-only omits the flag and separator", Config{ProjectPath: "p", EnvOnlyActivation: true},
			[]string{"--path", "p"}},
		{"env-only keeps the host's own args", Config{ProjectPath: "p", EnvOnlyActivation: true, ExtraArgs: []string{"--x"}},
			[]string{"--path", "p", "--", "--x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := godotArgs(tc.cfg); !slices.Equal(got, tc.want) {
				t.Errorf("godotArgs = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := Config{
		ProjectPath: "/some/project",
	}
	if cfg.Host != "" {
		t.Errorf("expected empty host, got %q", cfg.Host)
	}
	if cfg.Port != 0 {
		t.Errorf("expected zero port, got %d", cfg.Port)
	}
	if cfg.Headless != false {
		t.Errorf("expected headless false, got %v", cfg.Headless)
	}
	if cfg.TimeoutMs != 0 {
		t.Errorf("expected zero timeout, got %d", cfg.TimeoutMs)
	}
}

func TestLaunchValidation(t *testing.T) {
	ctx := context.Background()
	_, err := Launch(ctx, Config{})
	if err == nil {
		t.Fatal("expected error for empty project_path")
	}
	if !contains(err.Error(), "project_path is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLaunchRejectsOutOfRangePort(t *testing.T) {
	ctx := context.Background()
	for _, port := range []int{-1, 65536, 99999} {
		_, err := Launch(ctx, Config{ProjectPath: "/some/project", Port: port})
		if err == nil {
			t.Fatalf("port %d: expected error, got nil", port)
		}
		if !contains(err.Error(), "1-65535") {
			t.Errorf("port %d: unexpected error: %v", port, err)
		}
	}
}

func TestNormalizeHostDefaultUsesDeterministicLoopback(t *testing.T) {
	if got := normalizeHost(""); got != "127.0.0.1" {
		t.Fatalf("normalizeHost(\"\") = %q, want 127.0.0.1", got)
	}
	if got := normalizeHost("  "); got != "127.0.0.1" {
		t.Fatalf("normalizeHost(\"  \") = %q, want 127.0.0.1", got)
	}
	if got := normalizeHost("localhost"); got != "localhost" {
		t.Fatalf("normalizeHost should preserve explicit host, got %q", got)
	}
}

func TestFindGodotBinaryEnv(t *testing.T) {
	// Set a dummy env var that points to a non-existent binary.
	// Should error because binary doesn't exist.
	t.Setenv("STAGEHAND_GODOT_BIN", "/nonexistent/godot")
	_, err := FindGodotBinary()
	if err == nil {
		t.Fatal("expected error for nonexistent binary")
	}
	// Set to a directory (should error)
	tmpDir := t.TempDir()
	t.Setenv("GODOT_BIN", tmpDir)
	_, err = FindGodotBinary()
	if err == nil {
		t.Fatal("expected error for directory")
	}
}

func TestResolveExecutable(t *testing.T) {
	// Test with absolute path to a file that doesn't exist.
	_, err := resolveExecutable("/tmp/nonexistent-godot")
	if err == nil {
		t.Fatal("expected error")
	}
	// Test with directory
	tmpDir := t.TempDir()
	_, err = resolveExecutable(tmpDir)
	if err == nil {
		t.Fatal("expected error for directory")
	}
	// Test with executable in PATH (we can use "echo" or "true")
	path, err := resolveExecutable("true")
	if err != nil {
		t.Skipf("true not found in PATH: %v", err)
	}
	if path == "" {
		t.Fatal("expected path")
	}
}

// TestLaunchCommandConstruction verifies that the command line arguments
// and environment variables are set correctly.
func TestLaunchCommandConstruction(t *testing.T) {
	t.Skip("flaky due to timing issues; covered by integration tests")
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "fake-godot")
	// Write a simple bash script that writes its arguments and env vars to output.
	script := `#!/bin/sh
echo "args: $@" > "$0.output"
echo "env: STAGEHAND_PORT=$STAGEHAND_PORT, STAGEHAND_ENABLED=$STAGEHAND_ENABLED" >> "$0.output"
# Hang forever to simulate a Godot process that doesn't start the websocket.
sleep 10
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	// Create a minimal project directory.
	projectDir := filepath.Join(tmpDir, "project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "project.godot"), []byte("config_version=5\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	cfg := Config{
		ProjectPath: projectDir,
		GodotBin:    scriptPath,
		Host:        "localhost",
		Port:        26702,
		Headless:    true,
		ExtraArgs:   []string{"--verbose"},
		TimeoutMs:   100, // short timeout, should fail quickly
	}
	_, err := Launch(ctx, cfg)
	// Expect timeout error because script hangs.
	if err == nil {
		t.Fatal("expected timeout error")
	}
	// Check that the script was invoked with correct arguments.
	data, err := os.ReadFile(scriptPath + ".output")
	if err != nil {
		t.Skipf("could not read output file: %v (maybe script didn't run)", err)
	}
	output := string(data)
	// Expect --headless, --path, --, --stagehand, --verbose
	if !strings.Contains(output, "--headless") {
		t.Errorf("output missing --headless: %s", output)
	}
	if !strings.Contains(output, "--path") {
		t.Errorf("output missing --path: %s", output)
	}
	if !strings.Contains(output, "--stagehand") {
		t.Errorf("output missing --stagehand: %s", output)
	}
	if !strings.Contains(output, "--verbose") {
		t.Errorf("output missing --verbose: %s", output)
	}
	// Expect environment variables
	if !strings.Contains(output, "STAGEHAND_PORT=26702") {
		t.Errorf("output missing STAGEHAND_PORT: %s", output)
	}
	if !strings.Contains(output, "STAGEHAND_ENABLED=1") {
		t.Errorf("output missing STAGEHAND_ENABLED: %s", output)
	}
}

// TestLaunchErrorExit verifies that Launch returns an error when the Godot binary exits with non-zero.
func TestLaunchErrorExit(t *testing.T) {
	t.Skip("flaky due to timing issues; covered by integration tests")
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "fake-godot")
	script := `#!/bin/sh
exit 1
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(tmpDir, "project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "project.godot"), []byte("config_version=5\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	cfg := Config{
		ProjectPath: projectDir,
		GodotBin:    scriptPath,
		TimeoutMs:   5000,
	}
	_, err := Launch(ctx, cfg)
	if err == nil {
		t.Fatal("expected error from binary exit")
	}
	// Should contain "failed to start" or "Godot failed to become ready"
	if !contains(err.Error(), "failed") && !contains(err.Error(), "Godot failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestLaunchTimeout verifies that Launch returns a timeout error when the Godot process
// starts but never opens the WebSocket port.
func TestLaunchTimeout(t *testing.T) {
	t.Skip("flaky due to timing issues; covered by integration tests")
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "fake-godot")
	script := `#!/bin/sh
# Simulate a Godot process that runs but doesn't open a WebSocket.
# Sleep longer than timeout.
sleep 2
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(tmpDir, "project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "project.godot"), []byte("config_version=5\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	cfg := Config{
		ProjectPath: projectDir,
		GodotBin:    scriptPath,
		Port:        26703,
		TimeoutMs:   100, // very short timeout
	}
	_, err := Launch(ctx, cfg)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !contains(err.Error(), "timeout") && !contains(err.Error(), "timed out") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// TestWaitForProcessExitReturnsOnSend verifies that waitForProcessExit
// unblocks as soon as the wait channel receives, well before the timeout.
func TestWaitForProcessExitReturnsOnSend(t *testing.T) {
	wait := make(chan error, 1)
	wait <- errors.New("exit status 1")

	start := time.Now()
	exited := waitForProcessExit(wait, 5*time.Second)
	elapsed := time.Since(start)

	if !exited {
		t.Fatal("expected waitForProcessExit to report exit")
	}
	if elapsed > time.Second {
		t.Fatalf("waitForProcessExit took %v, expected near-instant return", elapsed)
	}
}

// TestWaitForProcessExitTimesOut verifies that waitForProcessExit gives up
// after timeout instead of blocking forever on a process that never sends,
// which is the LOW finding this bounds (a killed process that ignores
// SIGKILL, e.g. stuck in uninterruptible I/O, must not hang launch cleanup).
func TestWaitForProcessExitTimesOut(t *testing.T) {
	wait := make(chan error) // never sent to

	exited := waitForProcessExit(wait, 20*time.Millisecond)

	if exited {
		t.Fatal("expected waitForProcessExit to report timeout, not exit")
	}
}

// TestKillAfterExitStillRemovesUserDataDir pins that a process which already
// exited (os.ErrProcessDone from Process.Kill) is a successful kill, not an
// early return that leaks the temp user-data dir.
func TestKillAfterExitStillRemovesUserDataDir(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start true: %v", err)
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	// Let the process be reaped so Process.Kill reports it as finished.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(wait) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	owned := filepath.Join(t.TempDir(), "godot-stagehand-userdata-x")
	if err := os.Mkdir(owned, 0o755); err != nil {
		t.Fatal(err)
	}

	r := &LaunchResult{Process: cmd, waitChan: wait, ownedUserDataDir: owned}
	if err := r.Kill(); err != nil {
		t.Fatalf("Kill after exit = %v, want nil", err)
	}
	if _, err := os.Stat(owned); !os.IsNotExist(err) {
		t.Errorf("owned user data dir still present after Kill (stat err = %v)", err)
	}
}
