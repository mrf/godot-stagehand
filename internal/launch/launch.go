package launch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
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
	// Host is the hostname to connect to (default "127.0.0.1").
	Host string
	// Port is the TCP port for the WebSocket server (default 26700).
	Port int
	// Headless launches Godot in headless mode (--headless flag). Default true.
	Headless bool
	// AllowUnsafe enables expression evaluation and arbitrary method calls.
	AllowUnsafe bool
	// ExtraArgs are additional command-line arguments passed to the Godot binary.
	ExtraArgs []string
	// TimeoutMs is the maximum time to wait for Godot to start and become ready, in milliseconds.
	TimeoutMs int
}

const (
	// DefaultHost is the deterministic local loopback address used when no host is supplied.
	DefaultHost = "127.0.0.1"
	// defaultTimeout is used when TimeoutMs is zero.
	defaultTimeout = 30000 // 30 seconds
)

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
	host = normalizeHost(host)
	port := cfg.Port
	if port == 0 {
		port = 26700
	}
	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
	if timeout == 0 {
		timeout = defaultTimeout * time.Millisecond
	}

	// Fail fast if something already holds the port. Otherwise the process we
	// spawn cannot bind it, and dialGodotWhenReady would happily connect to the
	// pre-existing squatter instead — silently attaching to the wrong process.
	if err := assertPortFree(host, port); err != nil {
		return nil, err
	}

	// Generate a per-launch nonce so we can prove the process we connect to is
	// the one we spawned: we pass it via env and the addon echoes it back in the
	// ping response. A different instance holding the port would echo a
	// different (or empty) token, which we reject below.
	instanceToken, err := newInstanceToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate instance token: %w", err)
	}
	authToken, err := newInstanceToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate authentication token: %w", err)
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

	unsafeSetting := "0"
	if cfg.AllowUnsafe {
		unsafeSetting = "1"
	}
	cmd := exec.Command(godotBin, args...)
	cmd.SysProcAttr = sysProcAttr()
	cmd.Env = append(os.Environ(),
		"STAGEHAND_ENABLED=1",
		fmt.Sprintf("STAGEHAND_PORT=%d", port),
		fmt.Sprintf("STAGEHAND_INSTANCE_TOKEN=%s", instanceToken),
		fmt.Sprintf("STAGEHAND_AUTH_TOKEN=%s", authToken),
		fmt.Sprintf("STAGEHAND_ALLOW_UNSAFE=%s", unsafeSetting),
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
	// We will keep the connection open on success; caller is responsible for
	// closing it. cleanup tears everything down on any post-connect failure.
	cleanup := func() {
		conn.Close()
		_ = cmd.Process.Kill()
		<-wait
	}
	if err := conn.Authenticate(launchCtx, authToken); err != nil {
		cleanup()
		return nil, fmt.Errorf("Stagehand authentication failed after launch: %w", err)
	}

	// Get engine info via ping.
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	pingResp, err := conn.Call(pingCtx, "ping", nil)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("ping failed after launch: %w", err)
	}
	if pingResp.Error != nil {
		cleanup()
		return nil, fmt.Errorf("ping returned error: %v", pingResp.Error)
	}

	var ping struct {
		Status           string `json:"status"`
		Engine           string `json:"engine"`
		EngineVersion    string `json:"engine_version"`
		StagehandVersion string `json:"stagehand_version"`
		InstanceToken    string `json:"instance_token"`
	}
	if err := json.Unmarshal(pingResp.Result, &ping); err != nil {
		cleanup()
		return nil, fmt.Errorf("malformed ping response: %w", err)
	}
	if ping.Status != "ok" || ping.Engine != "godot" {
		cleanup()
		return nil, fmt.Errorf("unexpected ping response: status=%q, engine=%q", ping.Status, ping.Engine)
	}
	// Prove the instance we connected to is the one we spawned. If the token
	// does not match, we reached a different Stagehand instance (e.g. a stale
	// process that still holds the port) and our own process failed to bind.
	if err := verifyInstanceToken(ping.InstanceToken, instanceToken, host, port); err != nil {
		cleanup()
		return nil, err
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

func normalizeHost(host string) string {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return DefaultHost
	}
	return trimmed
}

// newInstanceToken returns a random hex token used to prove that the process we
// spawn is the one we end up connected to.
func newInstanceToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// assertPortFree returns an actionable error if something is already listening
// on host:port. This catches the common case where a stale Godot instance still
// holds the port, before we spawn a new process that cannot bind it.
func assertPortFree(host string, port int) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		// Nothing accepting connections: the port is free to use.
		return nil
	}
	_ = conn.Close()
	return fmt.Errorf("port %d on %s is already in use; another Godot/Stagehand instance is likely still running — free the port (kill the stale instance) or choose another port before launching", port, host)
}

// verifyInstanceToken asserts that the token echoed by the addon in its ping
// response matches the per-launch token we passed via env. A mismatch (or empty
// echoed token) means we connected to a different instance than the one we
// spawned.
func verifyInstanceToken(got, want, host string, port int) error {
	if got == want {
		return nil
	}
	return fmt.Errorf("connected to a different Stagehand instance on %s:%d (instance token mismatch): the process we launched failed to bind the port — free the port (kill the stale instance) or choose another port", host, port)
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
