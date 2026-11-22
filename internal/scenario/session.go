package scenario

import (
	"context"
	"fmt"
	"io"
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
	// Warnings are launch-time notices the report must carry, e.g. the game
	// listening off-loopback.
	Warnings []string

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

// launchConfig maps the launch-mode target onto launch.Config. A non-empty
// godotBinOverride (Options.GodotBin) wins over target.godot_bin.
func (sc *Scenario) launchConfig(godotBinOverride string, port int, logs io.Writer) launch.Config {
	target := sc.Target
	godotBin := target.GodotBin
	if godotBinOverride != "" {
		godotBin = godotBinOverride
	}
	return launch.Config{
		ProjectPath:       sc.resolve(target.ProjectPath),
		GodotBin:          godotBin,
		Host:              target.Host,
		Port:              port,
		Headless:          target.headless(),
		AllowUnsafe:       target.AllowUnsafe,
		ShareUserData:     target.ShareUserData,
		ExtraArgs:         target.ExtraArgs,
		EnvOnlyActivation: target.EnvOnlyActivation,
		TimeoutMs:         target.TimeoutMs,
		LogWriter:         logs,
	}
}

func launchSession(ctx context.Context, sc *Scenario, opts Options, logs io.Writer) (*Session, error) {
	target := sc.Target
	port := 0
	if target.Port != nil {
		port = *target.Port
	}
	if port == 0 {
		free, err := launch.FreePort()
		if err != nil {
			return nil, fmt.Errorf("find a free port: %w", err)
		}
		port = free
	}

	result, err := launch.Launch(ctx, sc.launchConfig(opts.GodotBin, port, logs))
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
		Warnings: sessionWarnings(result),
		close: func() error {
			_ = result.Conn.Close()
			return result.Kill()
		},
	}
	return session, nil
}

// sessionWarnings collects the launch notices a report must not drop.
func sessionWarnings(result *launch.LaunchResult) []string {
	var warnings []string
	if result.BindWarning != "" {
		warnings = append(warnings, result.BindWarning)
	}
	if result.UserDataWarning != "" {
		warnings = append(warnings, result.UserDataWarning)
	}
	return warnings
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

// targetFromConfig builds the report's target block from the scenario's own
// configuration, before any session is opened. A failure to connect never
// reaches Session, so this is the only source for the host/port a failed run
// tried to reach; a successful dial overwrites it with the session's actual
// values.
func targetFromConfig(target Target) ReportTarget {
	host := target.Host
	if host == "" {
		host = launch.DefaultHost
	}
	port := 0
	if target.Port != nil {
		port = *target.Port
	}
	return ReportTarget{Mode: target.Mode, Host: host, Port: port}
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

// connectDeadline bounds a connect-mode handshake.
func connectDeadline(target Target) time.Duration {
	if target.TimeoutMs > 0 {
		return time.Duration(target.TimeoutMs) * time.Millisecond
	}
	return 30 * time.Second
}
