package scenario

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mrf/godot-stagehand/internal/gwp"
	"github.com/mrf/godot-stagehand/internal/gwpop"
	"github.com/mrf/godot-stagehand/internal/version"
	"github.com/mrf/godot-stagehand/internal/visual"
)

// Options configure one scenario run.
type Options struct {
	// OutDir receives every artifact: report.json, junit.xml, rpc-trace.json,
	// godot.log, screenshots and diff images. Empty writes no artifacts.
	OutDir string
	// JSONPath and JUnitPath override the default locations inside OutDir.
	JSONPath  string
	JUnitPath string
	// BaselineDir overrides the scenario's baseline directory.
	BaselineDir string
	// GodotBin overrides binary discovery for launch-mode targets.
	GodotBin string
	// AuthToken overrides the scenario's connect-mode token.
	AuthToken string
	// Timeout bounds the whole run. Zero means no runner-imposed bound; the
	// per-step deadlines still apply.
	Timeout time.Duration
	// Progress receives one line per step. Nil is silent.
	Progress io.Writer

	// dial and now are test seams.
	dial dialer
	now  func() time.Time
}

// Default artifact filenames inside OutDir.
const (
	reportFile = "report.json"
	junitFile  = "junit.xml"
	traceFile  = "rpc-trace.json"
	godotLog   = "godot.log"
)

// Run executes a scenario end to end. The returned error is non-nil only when
// the run could not produce a report at all; a failed scenario is reported as
// a Report with Status "failed", because a CI caller needs the artifacts of a
// failure far more than it needs a Go error.
func Run(ctx context.Context, sc *Scenario, opts Options) (*Report, error) {
	if err := sc.Validate(); err != nil {
		return nil, err
	}
	if opts.now == nil {
		opts.now = time.Now
	}
	if opts.dial == nil {
		opts.dial = openSession
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	run := &runState{sc: sc, opts: opts, started: opts.now()}
	report := run.execute(ctx)
	if err := run.writeArtifacts(report); err != nil {
		// An unwritable artifact directory is a real CI failure: the report the
		// pipeline is supposed to read does not exist.
		if report.Failure == nil {
			report.Status = StatusFailed
			report.Failure = &Failure{StepIndex: -1, Phase: "artifacts", Kind: KindInternal, Message: err.Error()}
		}
		return report, err
	}
	return report, nil
}

type runState struct {
	sc      *Scenario
	opts    Options
	started time.Time
	trace   *tracer

	baselineDir string
	artifactDir string
	diffDir     string
}

func (r *runState) execute(ctx context.Context) *Report {
	report := &Report{
		Name:             r.sc.Name,
		Description:      r.sc.Description,
		Status:           StatusPassed,
		StartedAt:        r.started.UTC().Format(time.RFC3339Nano),
		StagehandVersion: version.Version,
		Protocol:         gwp.ProtocolID,
		Target:           ReportTarget{Mode: r.sc.Target.Mode},
		Artifacts:        map[string]string{},
	}
	r.resolveDirs()

	logs, logPath, err := r.openGodotLog()
	if err != nil {
		return r.finish(report, &Failure{StepIndex: -1, Phase: "setup", Kind: KindInternal, Message: err.Error()})
	}
	if logs != nil {
		defer logs.Close()
		report.Artifacts["godot_log"] = logPath
	}

	session, err := r.opts.dial(ctx, r.sc, r.opts, logs)
	if err != nil {
		report.RPC = Trace{Calls: []TraceCall{}}
		return r.finish(report, &Failure{StepIndex: -1, Phase: "connect", Kind: KindConnection, Message: err.Error()})
	}
	defer func() { _ = session.Close() }()

	report.EngineVersion = session.Engine
	report.AddonVersion = session.Addon
	if session.Protocol != "" {
		report.Protocol = session.Protocol
	}
	report.Target.Host = session.Host
	report.Target.Port = session.Port
	report.Target.PID = session.PID

	r.trace = newTracer(session.Caller, r.opts.now)

	var failure *Failure
	report.Steps, failure = r.runPhase(ctx, "step", r.sc.Steps, false)
	// Teardown always runs so a scenario can restore state even after a
	// failure; its own failures are reported but never mask the first one.
	teardown, teardownFailure := r.runPhase(ctx, "teardown", r.sc.Teardown, true)
	report.Teardown = teardown
	if failure == nil {
		failure = teardownFailure
	}

	report.RPC = r.trace.Trace()
	return r.finish(report, failure)
}

// runPhase executes a list of steps. When force is true every step runs
// regardless of earlier failures (teardown semantics).
func (r *runState) runPhase(ctx context.Context, phase string, steps []Step, force bool) ([]StepResult, *Failure) {
	results := make([]StepResult, 0, len(steps))
	var failure *Failure

	for i, step := range steps {
		if failure != nil && !force {
			results = append(results, StepResult{
				Index: i, Name: step.Label(), Action: step.Action, Status: StatusSkipped,
			})
			continue
		}
		result := r.runStep(ctx, phase, i, step)
		results = append(results, result)
		if result.Status == StatusFailed && failure == nil && !step.ContinueOnFailure {
			failure = &Failure{
				StepIndex: i, StepName: step.Label(), Phase: phase,
				Kind: result.ErrorKind, Message: result.Error,
			}
		}
	}
	return results, failure
}

func (r *runState) runStep(ctx context.Context, phase string, index int, step Step) StepResult {
	r.trace.setStep(phase, index)
	start := r.opts.now()
	result := StepResult{
		Index:     index,
		Name:      step.Label(),
		Action:    step.Action,
		Status:    StatusPassed,
		StartedAt: start.UTC().Format(time.RFC3339Nano),
	}

	outcome, err := r.dispatch(ctx, step)
	result.DurationMs = r.opts.now().Sub(start).Milliseconds()
	result.Result = outcome.result
	result.Artifacts = outcome.artifacts
	if err != nil {
		result.Status = StatusFailed
		result.Error = err.Error()
		result.ErrorKind = classify(err)
	}

	r.report(result)
	return result
}

// stepOutcome carries whatever a step produced beyond pass/fail.
type stepOutcome struct {
	result    json.RawMessage
	artifacts []string
}

// assertionError marks a step that reached Godot successfully but whose
// asserted condition did not hold — the difference between "the harness broke"
// and "the game is wrong".
type assertionError struct{ message string }

func (e *assertionError) Error() string { return e.message }

func assertionFailed(format string, args ...any) error {
	return &assertionError{message: fmt.Sprintf(format, args...)}
}

func classify(err error) string {
	var assertErr *assertionError
	if errors.As(err, &assertErr) {
		return KindAssertion
	}
	var opErr *gwpop.Error
	if errors.As(err, &opErr) {
		switch opErr.Kind {
		case gwpop.KindUsage:
			return KindUsage
		case gwpop.KindTimeout:
			return KindTimeout
		case gwpop.KindRemote:
			return KindRemote
		case gwpop.KindTransport:
			return KindConnection
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return KindTimeout
	}
	return KindInternal
}

// dispatch runs one step: a runner-local action, or a pass-through GWP call.
func (r *runState) dispatch(ctx context.Context, step Step) (stepOutcome, error) {
	switch step.Action {
	case ActionSleep:
		return stepOutcome{}, r.doSleep(ctx, step)
	case ActionScreenshot:
		return r.doScreenshot(ctx, step)
	case ActionSaveBaseline:
		return r.doSaveBaseline(ctx, step)
	case ActionScreenshotDiff:
		return r.doScreenshotDiff(ctx, step)
	case ActionAssertProperty:
		return r.doAssertProperty(ctx, step)
	case ActionAssertNodes:
		return r.doAssertNodeCount(ctx, step)
	}

	raw, err := gwpop.Execute(ctx, r.trace, gwpop.Op{Action: step.Action, Params: step.With})
	if err != nil {
		return stepOutcome{}, err
	}
	outcome := stepOutcome{result: raw}
	// assert_performance answers with a verdict rather than an error, so the
	// runner has to turn a false verdict into a failed step itself.
	if step.Action == "assert_performance" {
		perf, decodeErr := gwpop.DecodePerformance(raw)
		if decodeErr != nil {
			return outcome, decodeErr
		}
		if !perf.Passed {
			return outcome, assertionFailed("performance monitor %s = %g, want %s %g",
				perf.Monitor, perf.Value, perf.Op, perf.Threshold)
		}
	}
	return outcome, nil
}

func (r *runState) doSleep(ctx context.Context, step Step) error {
	ms, _ := asPositiveInt(step.With["duration_ms"])
	timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *runState) doScreenshot(ctx context.Context, step Step) (stepOutcome, error) {
	shot, err := gwpop.Capture(ctx, r.trace, asString(step.With["selector"]))
	if err != nil {
		return stepOutcome{}, err
	}
	name := asString(step.With["output"])
	if name == "" {
		name = fmt.Sprintf("%s.png", sanitize(step.Label()))
	}
	if r.artifactDir == "" {
		return stepOutcome{result: mustJSON(map[string]any{"width": shot.Width, "height": shot.Height})}, nil
	}
	path := filepath.Join(r.artifactDir, "screenshots", name)
	if err := writeArtifact(path, shot.PNG); err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{
		result:    mustJSON(map[string]any{"path": path, "width": shot.Width, "height": shot.Height}),
		artifacts: []string{path},
	}, nil
}

func (r *runState) doSaveBaseline(ctx context.Context, step Step) (stepOutcome, error) {
	nodeSelector := asString(step.With["selector"])
	shot, err := gwpop.Capture(ctx, r.trace, nodeSelector)
	if err != nil {
		return stepOutcome{}, err
	}
	outcome, err := visual.SaveBaseline(r.baselineDir, asString(step.With["name"]), nodeSelector, shot)
	if err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{result: mustJSON(outcome), artifacts: []string{outcome.Path}}, nil
}

func (r *runState) doScreenshotDiff(ctx context.Context, step Step) (stepOutcome, error) {
	nodeSelector := asString(step.With["selector"])
	shot, err := gwpop.Capture(ctx, r.trace, nodeSelector)
	if err != nil {
		return stepOutcome{}, err
	}
	outcome, err := visual.CompareBaseline(visual.DiffConfig{
		BaselineDir:      r.baselineDir,
		ArtifactDir:      r.diffDir,
		Name:             asString(step.With["name"]),
		Selector:         nodeSelector,
		Threshold:        asFloatOr(step.With["threshold"], 0),
		PixelSensitivity: asFloatOr(step.With["pixel_sensitivity"], 0),
	}, shot)
	if err != nil {
		return stepOutcome{}, err
	}

	result := stepOutcome{result: mustJSON(outcome)}
	if outcome.ActualImagePath != "" {
		result.artifacts = append(result.artifacts, outcome.ActualImagePath)
	}
	if outcome.DiffImagePath != "" {
		result.artifacts = append(result.artifacts, outcome.DiffImagePath)
	}
	if !outcome.Pass {
		return result, assertionFailed("visual regression against baseline %q\n%s", outcome.Name, outcome.Report())
	}
	return result, nil
}

func (r *runState) doAssertProperty(ctx context.Context, step Step) (stepOutcome, error) {
	raw, err := gwpop.Execute(ctx, r.trace, gwpop.Op{Action: "get_property", Params: map[string]any{
		"selector": step.With["selector"],
		"property": step.With["property"],
	}})
	if err != nil {
		return stepOutcome{}, err
	}
	actual, err := gwpop.PropertyValue(raw)
	if err != nil {
		return stepOutcome{result: raw}, err
	}

	operator := asString(step.With["operator"])
	expected := step.With["expected"]
	ok, err := gwpop.Compare(operator, actual, expected)
	outcome := stepOutcome{result: mustJSON(map[string]any{
		"selector": step.With["selector"],
		"property": step.With["property"],
		"operator": operator,
		"expected": expected,
		"actual":   actual,
		"passed":   ok && err == nil,
	})}
	if err != nil {
		return outcome, err
	}
	if !ok {
		return outcome, assertionFailed("%s.%s = %s, want %s %s",
			asString(step.With["selector"]), asString(step.With["property"]),
			render(actual), operator, render(expected))
	}
	return outcome, nil
}

func (r *runState) doAssertNodeCount(ctx context.Context, step Step) (stepOutcome, error) {
	raw, err := gwpop.Execute(ctx, r.trace, gwpop.Op{Action: "find", Params: map[string]any{
		"selector": step.With["selector"],
		"limit":    1,
	}})
	if err != nil {
		return stepOutcome{}, err
	}
	count, err := gwpop.NodeCount(raw)
	if err != nil {
		return stepOutcome{result: raw}, err
	}

	operator := asString(step.With["operator"])
	expected := step.With["expected"]
	ok, err := gwpop.Compare(operator, float64(count), expected)
	outcome := stepOutcome{result: mustJSON(map[string]any{
		"selector": step.With["selector"],
		"operator": operator,
		"expected": expected,
		"actual":   count,
		"passed":   ok && err == nil,
	})}
	if err != nil {
		return outcome, err
	}
	if !ok {
		return outcome, assertionFailed("selector %q matched %d nodes, want %s %s",
			asString(step.With["selector"]), count, operator, render(expected))
	}
	return outcome, nil
}

// ── artifacts and reporting ───────────────────────────────────────────────

func (r *runState) resolveDirs() {
	r.artifactDir = r.opts.OutDir
	if r.artifactDir != "" {
		r.diffDir = filepath.Join(r.artifactDir, "diffs")
	}

	switch {
	case r.opts.BaselineDir != "":
		r.baselineDir = r.opts.BaselineDir
	case r.sc.BaselineDir != "":
		r.baselineDir = r.sc.resolve(r.sc.BaselineDir)
	default:
		r.baselineDir = r.sc.resolve("stagehand-baselines")
	}
	if r.diffDir == "" {
		r.diffDir = "stagehand-diffs"
	}
}

func (r *runState) openGodotLog() (*os.File, string, error) {
	if r.opts.OutDir == "" || r.sc.Target.Mode != ModeLaunch {
		return nil, "", nil
	}
	if err := os.MkdirAll(r.opts.OutDir, 0o755); err != nil {
		return nil, "", fmt.Errorf("create output directory %s: %w", r.opts.OutDir, err)
	}
	path := filepath.Join(r.opts.OutDir, godotLog)
	file, err := os.Create(path)
	if err != nil {
		return nil, "", fmt.Errorf("create %s: %w", path, err)
	}
	return file, path, nil
}

func (r *runState) finish(report *Report, failure *Failure) *Report {
	finished := r.opts.now()
	report.FinishedAt = finished.UTC().Format(time.RFC3339Nano)
	report.DurationMs = finished.Sub(r.started).Milliseconds()
	if failure != nil {
		report.Status = StatusFailed
		report.Failure = failure
	}
	if report.RPC.Calls == nil {
		report.RPC.Calls = []TraceCall{}
	}
	return report
}

func (r *runState) writeArtifacts(report *Report) error {
	jsonPath := r.opts.JSONPath
	junitPath := r.opts.JUnitPath
	if r.opts.OutDir != "" {
		if jsonPath == "" {
			jsonPath = filepath.Join(r.opts.OutDir, reportFile)
		}
		if junitPath == "" {
			junitPath = filepath.Join(r.opts.OutDir, junitFile)
		}
	}

	if r.opts.OutDir != "" {
		tracePath := filepath.Join(r.opts.OutDir, traceFile)
		if err := report.WriteTrace(tracePath); err != nil {
			return err
		}
		report.Artifacts["rpc_trace"] = tracePath
	}
	if junitPath != "" {
		if err := report.WriteJUnit(junitPath); err != nil {
			return err
		}
		report.Artifacts["junit"] = junitPath
	}
	// The JSON report is written last so it records every other artifact path.
	if jsonPath != "" {
		report.Artifacts["report"] = jsonPath
		if err := report.WriteJSON(jsonPath); err != nil {
			return err
		}
	}
	return nil
}

func (r *runState) report(result StepResult) {
	if r.opts.Progress == nil {
		return
	}
	mark := "ok  "
	switch result.Status {
	case StatusFailed:
		mark = "FAIL"
	case StatusSkipped:
		mark = "skip"
	}
	fmt.Fprintf(r.opts.Progress, "%s  %02d %-20s %-6s %s\n",
		mark, result.Index, result.Action, time.Duration(result.DurationMs)*time.Millisecond, result.Name)
	if result.Error != "" {
		fmt.Fprintf(r.opts.Progress, "      %s\n", result.Error)
	}
}

func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(fmt.Sprintf("{%q:%q}", "encode_error", err.Error()))
	}
	return data
}

func render(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}

func sanitize(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "step"
	}
	return string(out)
}
