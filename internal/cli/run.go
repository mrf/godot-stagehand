package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/mrf/godot-stagehand/internal/scenario"
)

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

// cmdRun is the scenario runner entry point. It deliberately does NOT take the
// shared connection flags: how to reach Godot is part of the scenario file, so
// a scenario is reproducible from the file alone.
var cmdRun = &command{
	name: "run", usage: "<scenario.json> [--out-dir DIR]", connects: false,
	summary: "Run a scenario file end to end and emit JSON, JUnit and artifacts",
	run: func(ctx context.Context, e *env, cmd *command, args []string) error {
		var outDir, jsonPath, junitPath, baselineDir, godotBin, token string
		var timeout time.Duration
		var quiet bool
		bind := func(fset *flag.FlagSet) {
			fset.StringVar(&outDir, "out-dir", "", "directory for report.json, junit.xml, rpc-trace.json, godot.log, screenshots and diffs")
			fset.StringVar(&jsonPath, "json", "", "write the JSON report here instead of <out-dir>/report.json")
			fset.StringVar(&junitPath, "junit", "", "write the JUnit XML here instead of <out-dir>/junit.xml")
			fset.StringVar(&baselineDir, "baseline-dir", "", "override the scenario's screenshot baseline directory")
			fset.StringVar(&godotBin, "godot-bin", "", "Godot binary for launch-mode scenarios (default: GODOT_BIN or PATH)")
			fset.StringVar(&token, "token", "", "session secret for connect-mode scenarios")
			fset.DurationVar(&timeout, "timeout", 0, "bound the whole run (default: none beyond per-step deadlines)")
			fset.BoolVar(&quiet, "quiet", false, "suppress per-step progress output")
		}

		return runWithFlags(ctx, cmd, e, args, bind, func(ctx context.Context, _ *session, fset *flag.FlagSet) error {
			if err := requireArgs(cmd, fset.Args(), 1, "<scenario.json>"); err != nil {
				return err
			}
			sc, err := scenario.Load(fset.Arg(0))
			if err != nil {
				return usagef(err)
			}

			var progress io.Writer
			if !quiet {
				progress = e.stderr
			}
			report, err := scenario.Run(ctx, sc, scenario.Options{
				OutDir:      outDir,
				JSONPath:    jsonPath,
				JUnitPath:   junitPath,
				BaselineDir: baselineDir,
				GodotBin:    godotBin,
				AuthToken:   token,
				Timeout:     timeout,
				Progress:    progress,
			})
			if report == nil {
				return usagef(err)
			}

			// The report goes to stdout so a pipeline can consume it without an
			// out-dir; progress and the summary go to stderr so the two never mix.
			if emitErr := emit(e.stdout, report); emitErr != nil {
				return emitErr
			}
			fmt.Fprintln(e.stderr, report.Summary())

			if err != nil {
				return err
			}
			if !report.Passed() {
				return &scenarioFailure{code: exitCodeForFailure(report.Failure), report: report}
			}
			return nil
		})
	},
}

// scenarioFailure carries a failed run's exit code. The report has already
// been printed by the time it surfaces, so its message is a one-line pointer
// rather than a repeat of the diagnostics.
type scenarioFailure struct {
	code   int
	report *scenario.Report
}

func (e *scenarioFailure) Error() string {
	if e.report.Failure == nil {
		return fmt.Sprintf("scenario %q failed", e.report.Name)
	}
	return fmt.Sprintf("scenario %q failed at %s step %d (%s): %s",
		e.report.Name, e.report.Failure.Phase, e.report.Failure.StepIndex,
		e.report.Failure.Kind, e.report.Failure.Message)
}

// ExitCode lets exitCodeFor honour the scenario's own classification.
func (e *scenarioFailure) ExitCode() int { return e.code }
