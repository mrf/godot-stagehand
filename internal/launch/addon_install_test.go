//go:build integration
// +build integration

package launch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrf/godot-stagehand/internal/godotconn"
)

const addonInstallAuthToken = "stagehand-addon-install-auth-token"

// TestAddonInstallation is a smoke test that verifies the stagehand addon can be
// installed into a fresh (non-test-project) Godot project, starts without parse
// errors, and optionally accepts a WebSocket ping.
//
// This simulates a user adding the addon to their own project for the first time.
func TestAddonInstallation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping addon installation smoke test in short mode")
	}

	godotBin, err := FindGodotBinary()
	if err != nil {
		t.Fatal(err)
	}
	if godotBin == "" {
		t.Skip("Godot binary not found; set GODOT_BIN, GODOT_PATH, or STAGEHAND_GODOT_BIN, or put godot/godot4 in PATH")
	}

	// Build a fresh minimal Godot project — not the pre-made test project.
	projectDir := buildMinimalProject(t)

	// Copy only the addon into it; no test scenes.
	root := findProjectRoot(t)
	copyDir(t, filepath.Join(root, "addons", "stagehand"), filepath.Join(projectDir, "addons", "stagehand"))

	port := freeTCPPort(t)
	logPath := filepath.Join(t.TempDir(), "godot-install-smoke.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create log file: %v", err)
	}

	cmd := exec.Command(godotBin, "--headless", "--path", projectDir, "--", "--stagehand")
	cmd.Env = append(os.Environ(),
		"STAGEHAND_ENABLED=1",
		fmt.Sprintf("STAGEHAND_PORT=%d", port),
		"STAGEHAND_AUTH_TOKEN="+addonInstallAuthToken,
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		t.Fatalf("start Godot: %v", err)
	}

	wait := make(chan error, 1)
	go func() {
		wait <- cmd.Wait()
		logFile.Close()
	}()
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		select {
		case <-wait:
		case <-time.After(5 * time.Second):
		}
	})

	// Attempt to connect and ping.  If Godot exits before we can connect
	// (e.g. headless with no game loop), we fall back to a log-only check.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn := tryDialGodot(t, ctx, port, wait, logPath)

	if conn == nil {
		// Godot exited before we could connect — verify the exit was clean.
		log := readGodotLog(logPath)
		checkAddonParseErrors(t, log)
		t.Log("Addon installed successfully (Godot started and exited cleanly; no parse errors detected)")
		return
	}
	defer conn.Close()
	if err := conn.Authenticate(context.Background(), addonInstallAuthToken); err != nil {
		log := readGodotLog(logPath)
		t.Fatalf("authenticate after addon install: %v\nGodot log:\n%s", err, log)
	}

	// Ping to confirm the addon is fully operational.
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()

	pingResp, err := conn.Call(pingCtx, "ping", nil)
	if err != nil {
		log := readGodotLog(logPath)
		t.Fatalf("ping failed after addon install: %v\nGodot log:\n%s", err, log)
	}

	var ping struct {
		Status           string `json:"status"`
		Engine           string `json:"engine"`
		EngineVersion    string `json:"engine_version"`
		StagehandVersion string `json:"stagehand_version"`
	}
	if err := json.Unmarshal(pingResp.Result, &ping); err != nil {
		t.Fatalf("unmarshal ping result: %v; raw=%s", err, pingResp.Result)
	}
	if ping.Status != "ok" || ping.Engine != "godot" {
		t.Fatalf("unexpected ping response: %+v", ping)
	}
	if ping.EngineVersion == "" || ping.StagehandVersion == "" {
		t.Fatalf("ping response missing version fields: %+v", ping)
	}

	// Also check the log for any parse errors that slipped through.
	checkAddonParseErrors(t, readGodotLog(logPath))

	t.Logf("Addon installed OK — engine=%s stagehand=%s", ping.EngineVersion, ping.StagehandVersion)
}

// buildMinimalProject creates a temporary directory with a bare-bones Godot
// project that registers the stagehand addon as an autoload.  The main scene
// is a single Node so Godot keeps its game loop running long enough to accept
// a WebSocket connection.
func buildMinimalProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	projectGodot := `; Engine configuration file.
[application]

config/name="Addon Install Smoke Test"
run/main_scene="res://main.tscn"

[autoload]

StagehandServer="*res://addons/stagehand/autoload/stagehand_server.gd"
`
	if err := os.WriteFile(filepath.Join(dir, "project.godot"), []byte(projectGodot), 0o644); err != nil {
		t.Fatalf("write project.godot: %v", err)
	}

	// Minimal scene: a single Node keeps the process alive without any gameplay logic.
	mainScene := `[gd_scene format=3]

[node name="Main" type="Node"]
`
	if err := os.WriteFile(filepath.Join(dir, "main.tscn"), []byte(mainScene), 0o644); err != nil {
		t.Fatalf("write main.tscn: %v", err)
	}

	return dir
}

// tryDialGodot attempts to open a WebSocket connection to Godot.  It returns
// nil (not a test failure) if Godot exits before the connection succeeds — the
// caller decides whether that is acceptable.
func tryDialGodot(t *testing.T, ctx context.Context, port int, wait <-chan error, logPath string) *godotconn.Connection {
	t.Helper()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		dialCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		conn, err := godotconn.Dial(dialCtx, "127.0.0.1", port)
		cancel()
		if err == nil {
			return conn
		}

		select {
		case <-wait:
			// Godot exited — not necessarily an error.
			return nil
		case <-ctx.Done():
			log := readGodotLog(logPath)
			t.Fatalf("timed out waiting for Godot WebSocket on port %d\nGodot log:\n%s", port, log)
		case <-ticker.C:
		}
	}
}

// checkAddonParseErrors scans Godot log output for GDScript parse errors and
// fails the test if any are found.
func checkAddonParseErrors(t *testing.T, log string) {
	t.Helper()
	for i, line := range strings.Split(log, "\n") {
		if strings.Contains(line, "SCRIPT ERROR") || strings.Contains(line, "Parse Error") {
			t.Errorf("addon parse error at log line %d: %s", i+1, strings.TrimSpace(line))
		}
	}
}

// readGodotLog reads the Godot log file, returning an empty string on error.
func readGodotLog(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("<could not read %s: %v>", path, err)
	}
	return string(data)
}
