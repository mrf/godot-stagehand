package hostcompat

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mrf/godot-stagehand/internal/launch"
)

// LaunchConfig is what LaunchAndProbe needs to start a host.
type LaunchConfig struct {
	// ProjectPath is the host's project directory (contains project.godot).
	ProjectPath string
	// GodotBin overrides binary discovery (GODOT_BIN, PATH).
	GodotBin string
	// Headed launches with a display. The default is headless, which is what
	// CI has; note a headless run reports what a headless run is, e.g.
	// embedded subwindows may not embed without a real display server.
	Headed bool
	// ExtraArgs are passed to the game after "--", e.g. a host's own flags.
	ExtraArgs []string
	// EnvOnlyActivation drops --stagehand for a host that parses its own argv
	// and rejects unknown flags; STAGEHAND_ENABLED alone activates the addon.
	EnvOnlyActivation bool
	// TimeoutMs bounds the launch, including a cold import. Zero uses the
	// launcher's default, which is too short for a large project's first run.
	TimeoutMs int
	// LogWriter receives the engine's output; nil discards it.
	LogWriter io.Writer
}

// LaunchError marks a failure to bring the host up, as opposed to a failure to
// read it once it was running. Frontends map the two to different exit codes.
type LaunchError struct{ err error }

func (e *LaunchError) Error() string { return e.err.Error() }
func (e *LaunchError) Unwrap() error { return e.err }

// LaunchAndProbe starts the host on its own port, runs Collect against it and
// tears it down. The time-to-ready it records covers the whole launch, import
// included, which is the cost the large-project axis is about.
func LaunchAndProbe(ctx context.Context, cfg LaunchConfig) (*Probe, error) {
	port, err := launch.FreePort()
	if err != nil {
		return nil, fmt.Errorf("find a free port: %w", err)
	}

	// Stat before launching: the launch itself creates the cache.
	_, statErr := os.Stat(filepath.Join(cfg.ProjectPath, ".godot", "imported"))
	cold := os.IsNotExist(statErr)

	start := time.Now()
	result, err := launch.Launch(ctx, launch.Config{
		ProjectPath:       cfg.ProjectPath,
		GodotBin:          cfg.GodotBin,
		Port:              port,
		Headless:          !cfg.Headed,
		ExtraArgs:         cfg.ExtraArgs,
		TimeoutMs:         cfg.TimeoutMs,
		LogWriter:         cfg.LogWriter,
		EnvOnlyActivation: cfg.EnvOnlyActivation,
	})
	if err != nil {
		return nil, &LaunchError{err: err}
	}
	ready := time.Since(start)
	defer func() {
		_ = result.Conn.Close()
		_ = result.Kill()
	}()

	probe, err := Collect(ctx, result.Conn)
	if err != nil {
		return nil, err
	}
	probe.TimeToReadyMs = ready.Milliseconds()
	probe.ColdImport = cold
	return probe, nil
}
