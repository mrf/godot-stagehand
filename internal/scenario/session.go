package scenario

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/mrf/godot-stagehand/internal/gwp"
	"github.com/mrf/godot-stagehand/internal/gwpop"
	"github.com/mrf/godot-stagehand/internal/launch"
)

// Session is a live Godot connection the runner drives.
type Session struct {
	Caller   gwpop.Caller
	Host     string
	Port     int
	PID      int
	Engine   string
	Addon    string
	Protocol string

	close func() error
}

// Close tears the session down, killing a launched process.
func (s *Session) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// dialer opens a session for a target. It is a field on Options so tests can
// exercise the whole runner against a stub Godot without a real engine.
type dialer func(ctx context.Context, sc *Scenario, opts Options, logs io.Writer) (*Session, error)

func openSession(ctx context.Context, sc *Scenario, opts Options, logs io.Writer) (*Session, error) {
	switch sc.Target.Mode {
	case ModeLaunch:
		return launchSession(ctx, sc, opts, logs)
	case ModeConnect:
		return connectSession(ctx, sc, opts)
	default:
		return nil, fmt.Errorf("unknown target.mode %q", sc.Target.Mode)
	}
}

func launchSession(ctx context.Context, sc *Scenario, opts Options, logs io.Writer) (*Session, error) {
	target := sc.Target
	port := 0
	if target.Port != nil {
		port = *target.Port
	}
	if port == 0 {
		free, err := freePort()
		if err != nil {
			return nil, fmt.Errorf("find a free port: %w", err)
		}
		port = free
	}

	godotBin := target.GodotBin
	if opts.GodotBin != "" {
		godotBin = opts.GodotBin
	}

	result, err := launch.Launch(ctx, launch.Config{
		ProjectPath:   sc.resolve(target.ProjectPath),
		GodotBin:      godotBin,
		Host:          target.Host,
		Port:          port,
		Headless:      target.headless(),
		AllowUnsafe:   target.AllowUnsafe,
		ShareUserData: target.ShareUserData,
		ExtraArgs:     target.ExtraArgs,
		TimeoutMs:     target.TimeoutMs,
		LogWriter:     logs,
	})
	if err != nil {
		return nil, err
	}

	session := &Session{
		Caller:   result.Conn,
		Host:     result.Host,
		Port:     result.Port,
		PID:      result.PID,
		Engine:   result.EngineVersion,
		Addon:    result.StagehandVersion,
		Protocol: gwp.ProtocolID,
		close: func() error {
			_ = result.Conn.Close()
			return result.Kill()
		},
	}
	return session, nil
}

func connectSession(ctx context.Context, sc *Scenario, opts Options) (*Session, error) {
	target := sc.Target
	host := target.Host
	if host == "" {
		host = launch.DefaultHost
	}
	port := *target.Port

	token, err := resolveToken(target, opts)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, connectDeadline(target))
	defer cancel()

	conn, handshake, err := gwpop.Connect(ctx, host, port, token)
	if err != nil {
		return nil, err
	}

	return &Session{
		Caller:   conn,
		Host:     host,
		Port:     port,
		Engine:   handshake.EngineVersion,
		Addon:    handshake.StagehandVersion,
		Protocol: gwp.ProtocolID,
		close:    conn.Close,
	}, nil
}

// resolveToken finds the session secret, preferring an explicit CLI flag, then
// the scenario's named environment variable, then an inline token, then the
// conventional STAGEHAND_AUTH_TOKEN.
func resolveToken(target Target, opts Options) (string, error) {
	if opts.AuthToken != "" {
		return opts.AuthToken, nil
	}
	if target.TokenEnv != "" {
		token := os.Getenv(target.TokenEnv)
		if token == "" {
			return "", fmt.Errorf("target.token_env names %s but that variable is empty", target.TokenEnv)
		}
		return token, nil
	}
	if target.Token != "" {
		return target.Token, nil
	}
	if token := os.Getenv("STAGEHAND_AUTH_TOKEN"); token != "" {
		return token, nil
	}
	return "", fmt.Errorf("no authentication token: set --token, target.token_env, or STAGEHAND_AUTH_TOKEN")
}

// resolve makes a scenario-relative path absolute against the scenario's own
// directory, so a scenario file can be run from any working directory.
func (s *Scenario) resolve(path string) string {
	if path == "" || filepath.IsAbs(path) || s.dir == "" {
		return path
	}
	return filepath.Join(s.dir, path)
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// connectDeadline bounds a connect-mode handshake.
func connectDeadline(target Target) time.Duration {
	if target.TimeoutMs > 0 {
		return time.Duration(target.TimeoutMs) * time.Millisecond
	}
	return 30 * time.Second
}
