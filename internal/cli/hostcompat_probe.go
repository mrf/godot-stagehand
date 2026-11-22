package cli

import (
	"context"
	"errors"
	"flag"
	"time"

	"github.com/mrf/godot-stagehand/internal/hostcompat"
)

// cmdHostcompatProbe is the host-compat capability probe (design doc
// Amendment 7a). Like run, it owns its launch, so it takes no connection flags.
// A failure to bring the host up is a connection error (exit 3) — the same
// class `run` gives a launch failure — so CI can tell "the host would not
// start" from "the probe could not read it" (exit 4).
var cmdHostcompatProbe = &command{
	name: "hostcompat-probe", usage: "--project-path DIR [--godot-bin PATH] [--game-args a,b] [--env-only-activation]", connects: false,
	summary: "Launch a host project and report its measured stress axes as JSON",
	run: func(ctx context.Context, e *env, cmd *command, args []string) error {
		var projectPath, godotBin, gameArgs string
		var timeout time.Duration
		var headed, envOnly bool
		bind := func(fset *flag.FlagSet) {
			fset.StringVar(&projectPath, "project-path", "", "host project directory (required)")
			fset.StringVar(&godotBin, "godot-bin", "", "Godot binary (default: GODOT_BIN or PATH)")
			fset.StringVar(&gameArgs, "game-args", "", "comma-separated arguments passed to the game after --")
			fset.DurationVar(&timeout, "timeout", 10*time.Minute, "bound the launch (including a cold import) and the probe")
			fset.BoolVar(&envOnly, "env-only-activation", false, "activate via STAGEHAND_ENABLED only; omit --stagehand for hosts that reject unknown args")
			fset.BoolVar(&headed, "headed", false, "launch with a display instead of --headless")
		}
		return runWithFlags(ctx, cmd, e, args, bind, func(ctx context.Context, _ *session, fset *flag.FlagSet) error {
			if err := requireArgs(cmd, fset.Args(), 0, "no positional arguments"); err != nil {
				return err
			}
			if projectPath == "" {
				return usagef(errors.New("--project-path is required"))
			}
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			probe, err := hostcompat.LaunchAndProbe(ctx, hostcompat.LaunchConfig{
				ProjectPath:       projectPath,
				GodotBin:          godotBin,
				Headed:            headed,
				EnvOnlyActivation: envOnly,
				ExtraArgs:         splitList(gameArgs),
				TimeoutMs:         int(timeout / time.Millisecond),
			})
			var launchErr *hostcompat.LaunchError
			if errors.As(err, &launchErr) {
				return &connectionError{err: err}
			}
			if err != nil {
				return err
			}
			return emit(e.stdout, probe)
		})
	},
}
