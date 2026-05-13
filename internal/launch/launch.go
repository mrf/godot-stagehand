package launch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mrf/godot-stagehand/internal/godotconn"
)

// Config holds parameters for launching a Godot process.
type Config struct {
	// ProjectPath is the path to the Godot project directory (contains project.godot).
	ProjectPath string
	// GodotBin is the path to the Godot binary. If empty, findGodotBinary will attempt to locate it.
	GodotBin string
	// Host is the hostname to bind the WebSocket server (default "localhost").
	Host string
	// Port is the TCP port for the WebSocket server (default 26700).
	Port int
	// Headless launches Godot in headless mode (--headless flag). Default true.
	Headless bool
	// ExtraArgs are additional command-line arguments passed to the Godot binary.
	ExtraArgs []string
	// TimeoutMs is the maximum time to wait for Godot to start and become ready, in milliseconds.
	TimeoutMs int
}

// defaultTimeout is used when TimeoutMs is zero.
const defaultTimeout = 30000 // 30 seconds

// LaunchResult holds information about a successfully launched Godot process.
type LaunchResult struct {
	// PID is the process ID of the launched Godot process.
	PID int
	// Host is the hostname that the WebSocket server is listening on.
	Host string
	// Port is the port that the WebSocket server is listening on.
	Port int
	// EngineVersion is the Godot engine version reported by the stagehand addon ping.
	EngineVersion string
	// StagehandVersion is the stagehand addon version reported by ping.
	StagehandVersion string
	// Conn is the established WebSocket connection to the launched Godot instance.
	// Callers are responsible for closing this connection when done.
	Conn *godotconn.Connection
	// Process is the underlying os/exec.Cmd. Callers can use it to wait for termination.
	Process *exec.Cmd
	// waitChan is a channel that will receive the process exit error.
	waitChan <-chan error
}

// Launch starts a Godot process with the stagehand addon enabled, waits for the WebSocket
// server to become ready, and returns a connection and process information.
// The caller is responsible for cleaning up the process (call Kill() or Wait()).
func Launch(ctx context.Context, cfg Config) (*LaunchResult, error) {
	if cfg.ProjectPath == "" {
		return nil, fmt.Errorf("project_path is required")
	}
	godotBin := cfg.GodotBin
	if godotBin == "" {
		var err error
		godotBin, err = FindGodotBinary()
		if err != nil {
			return nil, fmt.Errorf("failed to locate Godot binary: %w", err)
		}
		if godotBin == "" {
			return nil, fmt.Errorf("Godot binary not found; set GODOT_BIN, GODOT_PATH, or STAGEHAND_GODOT_BIN, or put godot/godot4 in PATH")
		}
	}
	host := cfg.Host
	if host == "" {
		host = "localhost"
	}
	port := cfg.Port
	if port == 0 {
		port = 26700
	}
	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
	if timeout == 0 {
		timeout = defaultTimeout * time.Millisecond
	}

	// Prepare command line arguments.
	args := []string{}
	if cfg.Headless {
		args = append(args, "--headless")
	}
	args = append(args, "--path", cfg.ProjectPath)
	// Separate Godot arguments from stagehand arguments by "--"
	args = append(args, "--")
	args = append(args, "--stagehand")
	if len(cfg.ExtraArgs) > 0 {
		args = append(args, cfg.ExtraArgs...)
	}

	cmd := exec.Command(godotBin, args...)
	cmd.Env = append(os.Environ(),
		"STAGEHAND_ENABLED=1",
		fmt.Sprintf("STAGEHAND_PORT=%d", port),
	)
	// Redirect stdout/stderr to a log file (or discard?). For now, discard.
	// We could optionally capture logs, but we'll discard.
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start Godot: %w", err)
	}

	wait := make(chan error, 1)
	go func() {
		wait <- cmd.Wait()
	}()

	// Wait for the WebSocket server to become ready.
	launchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := dialGodotWhenReady(launchCtx, host, port, wait)
	if err != nil {
		// Clean up process on failure.
		_ = cmd.Process.Kill()
		<-wait // ensure goroutine completes
		return nil, fmt.Errorf("Godot failed to become ready: %w", err)
	}
	// We will keep the connection open; caller is responsible for closing it.

	// Get engine info via ping.
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	pingResp, err := conn.Call(pingCtx, "ping", nil)
	if err != nil {
		conn.Close()
		_ = cmd.Process.Kill()
		<-wait
		return nil, fmt.Errorf("ping failed after launch: %w", err)
	}
	if pingResp.Error != nil {
		conn.Close()
		_ = cmd.Process.Kill()
		<-wait
		return nil, fmt.Errorf("ping returned error: %v", pingResp.Error)
	}

	var ping struct {
		Status           string `json:"status"`
		Engine           string `json:"engine"`
		EngineVersion    string `json:"engine_version"`
		StagehandVersion string `json:"stagehand_version"`
	}
	if err := json.Unmarshal(pingResp.Result, &ping); err != nil {
		conn.Close()
		_ = cmd.Process.Kill()
		<-wait
		return nil, fmt.Errorf("malformed ping response: %w", err)
	}
	if ping.Status != "ok" || ping.Engine != "godot" {
		conn.Close()
		_ = cmd.Process.Kill()
		<-wait
		return nil, fmt.Errorf("unexpected ping response: status=%q, engine=%q", ping.Status, ping.Engine)
	}

	return &LaunchResult{
		PID:              cmd.Process.Pid,
		Host:             host,
		Port:             port,
		EngineVersion:    ping.EngineVersion,
		StagehandVersion: ping.StagehandVersion,
		Conn:             conn,
		Process:          cmd,
		waitChan:         wait,
	}, nil
}

// Kill terminates the Godot process and waits for it to exit.
func (r *LaunchResult) Kill() error {
	if r.Process == nil || r.Process.Process == nil {
		return nil
	}
	if err := r.Process.Process.Kill(); err != nil {
		return err
	}
	select {
	case <-r.waitChan:
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timed out waiting for Godot process to exit")
	}
	return nil
}

// Wait waits for the Godot process to exit and returns the exit error.
func (r *LaunchResult) Wait() error {
	if r.waitChan == nil {
		return fmt.Errorf("no wait channel")
	}
	return <-r.waitChan
}

// dialGodotWhenReady repeatedly attempts to connect to the Godot WebSocket server
// until success, the process exits, or the context expires.
func dialGodotWhenReady(ctx context.Context, host string, port int, wait <-chan error) (*godotconn.Connection, error) {
	var lastErr error
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		dialCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		conn, err := godotconn.Dial(dialCtx, host, port)
		cancel()
		if err == nil {
			return conn, nil
		}
		lastErr = err

		select {
		case err := <-wait:
			return nil, fmt.Errorf("Godot exited before accepting WebSocket connections: %w (last dial error: %v)", err, lastErr)
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out waiting for Godot WebSocket on %s:%d: %w (last dial error: %v)", host, port, ctx.Err(), lastErr)
		case <-ticker.C:
		}
	}
}

// findGodotBinary attempts to locate a Godot binary using environment variables
// and common executable names.
func FindGodotBinary() (string, error) {
	for _, envName := range []string{"STAGEHAND_GODOT_BIN", "GODOT_BIN", "GODOT_PATH"} {
		configured := strings.TrimSpace(os.Getenv(envName))
		if configured == "" {
			continue
		}
		path, err := resolveExecutable(configured)
		if err != nil {
			return "", fmt.Errorf("%s is set but does not resolve to an executable Godot binary: %w", envName, err)
		}
		return path, nil
	}

	for _, name := range []string{"godot", "godot4", "godot4.5", "godot4.4", "godot4.3", "godot4.2"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", nil
}

func resolveExecutable(configured string) (string, error) {
	if strings.ContainsAny(configured, `/\`) {
		info, err := os.Stat(configured)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s is a directory", configured)
		}
		return configured, nil
	}
	return exec.LookPath(configured)
}