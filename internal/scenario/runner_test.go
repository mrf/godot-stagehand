package scenario

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mrf/godot-stagehand/internal/godotconn"
	"github.com/mrf/godot-stagehand/internal/visual"
	"github.com/mrf/godot-stagehand/internal/visual/visualtest"
)

// stubGodot answers RPCs from a canned table, standing in for the addon.
type stubGodot struct {
	mu      sync.Mutex
	results map[string]any
	errs    map[string]error
	methods []string
	delay   time.Duration
}

func (s *stubGodot) Call(_ context.Context, method string, _ any) (*godotconn.Response, error) {
	s.mu.Lock()
	s.methods = append(s.methods, method)
	result, ok := s.results[method]
	err := s.errs[method]
	s.mu.Unlock()

	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	if err != nil {
		return nil, err
	}
	if !ok {
		result = map[string]any{"ok": true}
	}
	raw, merr := json.Marshal(result)
	if merr != nil {
		return nil, merr
	}
	return &godotconn.Response{JSONRPC: "2.0", ID: 1, Result: raw}, nil
}

func (s *stubGodot) called() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.methods...)
}

// fakeClock advances a fixed step per read, so durations in reports are
// deterministic and a test can assert on them.
func fakeClock(step time.Duration) func() time.Time {
	base := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	ticks := 0
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		now := base.Add(time.Duration(ticks) * step)
		ticks++
		return now
	}
}

func stubOptions(t *testing.T, godot *stubGodot) Options {
	t.Helper()
	return Options{
		OutDir: t.TempDir(),
		now:    fakeClock(10 * time.Millisecond),
		dial: func(_ context.Context, _ *Scenario, _ Options, _ io.Writer) (*Session, error) {
			return &Session{
				Caller: godot, Host: "127.0.0.1", Port: 26999, PID: 4242,
				Engine: "4.6.2.stable", Addon: "0.2.0", Protocol: "gwp/1",
			}, nil
		},
	}
}

func mustParse(t *testing.T, body string) *Scenario {
	t.Helper()
	sc, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return sc
}

func TestRunExecutesStepsInOrderAndReportsPass(t *testing.T) {
	godot := &stubGodot{results: map[string]any{
		"get_property": map[string]any{"value": "Ready", "type": "String"},
		"query_nodes":  map[string]any{"nodes": []any{}, "count": 3},
	}}
	sc := mustParse(t, `{
		"name": "menu smoke",
		"target": {"mode": "launch", "project_path": "proj"},
		"steps": [
			{"name": "root appears", "action": "wait_for_node", "with": {"selector": "class:Node", "timeout_ms": 500}},
			{"action": "click", "with": {"selector": "text:Start"}},
			{"action": "assert_property", "with": {"selector": "name:Label", "property": "text", "operator": "equals", "expected": "Ready"}},
			{"action": "assert_node_count", "with": {"selector": "class:Button", "operator": "greater_than", "expected": 2}}
		]
	}`)

	report, err := Run(context.Background(), sc, stubOptions(t, godot))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !report.Passed() {
		t.Fatalf("report status = %s, failure = %+v", report.Status, report.Failure)
	}
	if len(report.Steps) != 4 {
		t.Fatalf("recorded %d steps, want 4", len(report.Steps))
	}
	if got := report.Steps[0].Name; got != "root appears" {
		t.Errorf("step 0 name = %q, want the scenario-supplied name", got)
	}
	want := []string{"wait_for_node", "input_mouse", "get_property", "query_nodes"}
	got := godot.called()
	if len(got) != len(want) {
		t.Fatalf("methods = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("method %d = %q, want %q", i, got[i], want[i])
		}
	}
	if report.EngineVersion != "4.6.2.stable" || report.Target.PID != 4242 {
		t.Errorf("report does not carry session metadata: %+v", report.Target)
	}
}

func TestRunFailsAssertionAndSkipsRemainingSteps(t *testing.T) {
	godot := &stubGodot{results: map[string]any{
		"get_property": map[string]any{"value": "Loading", "type": "String"},
	}}
	sc := mustParse(t, `{
		"name": "assert",
		"target": {"mode": "launch", "project_path": "proj"},
		"steps": [
			{"action": "assert_property", "with": {"selector": "name:Label", "property": "text", "operator": "equals", "expected": "Ready"}},
			{"action": "click", "with": {"selector": "text:Next"}}
		]
	}`)

	report, err := Run(context.Background(), sc, stubOptions(t, godot))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Passed() {
		t.Fatal("report passed despite a failed assertion")
	}
	if report.Failure == nil || report.Failure.Kind != KindAssertion {
		t.Fatalf("failure = %+v, want an assertion failure", report.Failure)
	}
	if report.Steps[1].Status != StatusSkipped {
		t.Errorf("step 1 status = %s, want skipped", report.Steps[1].Status)
	}
	if !strings.Contains(report.Steps[0].Error, "Loading") {
		t.Errorf("failure message %q does not report the observed value", report.Steps[0].Error)
	}
	if contains(godot.called(), "input_mouse") {
		t.Error("a skipped step still reached Godot")
	}
}

func TestRunContinueOnFailureKeepsGoing(t *testing.T) {
	godot := &stubGodot{results: map[string]any{
		"get_property": map[string]any{"value": "Loading", "type": "String"},
	}}
	sc := mustParse(t, `{
		"name": "best effort",
		"target": {"mode": "launch", "project_path": "proj"},
		"steps": [
			{"action": "assert_property", "continue_on_failure": true, "with": {"selector": "name:L", "property": "text", "operator": "equals", "expected": "Ready"}},
			{"action": "click", "with": {"selector": "text:Next"}}
		]
	}`)

	report, err := Run(context.Background(), sc, stubOptions(t, godot))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Steps[1].Status != StatusPassed {
		t.Errorf("step 1 status = %s, want passed", report.Steps[1].Status)
	}
	// The run still reports the failed step; continue_on_failure only removes
	// the stop, never the record.
	if report.Steps[0].Status != StatusFailed {
		t.Errorf("step 0 status = %s, want failed", report.Steps[0].Status)
	}
}

func TestRunAlwaysExecutesTeardown(t *testing.T) {
	godot := &stubGodot{errs: map[string]error{"input_mouse": errors.New("write: broken pipe")}}
	sc := mustParse(t, `{
		"name": "teardown",
		"target": {"mode": "launch", "project_path": "proj"},
		"steps": [{"action": "click", "with": {"selector": "text:Boom"}}],
		"teardown": [{"action": "change_scene", "with": {"scene_path": "res://menu.tscn"}}]
	}`)

	report, err := Run(context.Background(), sc, stubOptions(t, godot))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Passed() {
		t.Fatal("report passed despite a transport failure")
	}
	if len(report.Teardown) != 1 || report.Teardown[0].Status != StatusPassed {
		t.Fatalf("teardown = %+v, want one passing step", report.Teardown)
	}
	if !contains(godot.called(), "change_scene") {
		t.Error("teardown did not reach Godot after a failed step")
	}
}

func TestRunSurfacesAddonReportedErrorAsRemote(t *testing.T) {
	godot := &stubGodot{results: map[string]any{
		"query_nodes": map[string]any{"error": "Node not found for selector: name:Ghost", "error_code": "no_match"},
	}}
	sc := mustParse(t, `{
		"name": "remote",
		"target": {"mode": "launch", "project_path": "proj"},
		"steps": [{"action": "find", "with": {"selector": "name:Ghost"}}]
	}`)

	report, err := Run(context.Background(), sc, stubOptions(t, godot))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Failure == nil || report.Failure.Kind != KindRemote {
		t.Fatalf("failure = %+v, want a remote failure", report.Failure)
	}
}

func TestRunReportsConnectionFailureWithoutSteps(t *testing.T) {
	opts := Options{
		OutDir: t.TempDir(),
		now:    fakeClock(time.Millisecond),
		dial: func(_ context.Context, _ *Scenario, _ Options, _ io.Writer) (*Session, error) {
			return nil, errors.New("Godot failed to become ready: timed out")
		},
	}
	sc := mustParse(t, `{
		"name": "unreachable",
		"target": {"mode": "launch", "project_path": "proj"},
		"steps": [{"action": "tree"}]
	}`)

	report, err := Run(context.Background(), sc, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Failure == nil || report.Failure.Kind != KindConnection {
		t.Fatalf("failure = %+v, want a connection failure", report.Failure)
	}
	if len(report.Steps) != 0 {
		t.Errorf("steps = %d, want none when the session never opened", len(report.Steps))
	}
	if _, err := os.Stat(filepath.Join(opts.OutDir, reportFile)); err != nil {
		t.Errorf("no report written for a connection failure: %v", err)
	}
}

func TestRunWritesEveryArtifact(t *testing.T) {
	godot := &stubGodot{results: map[string]any{
		"screenshot": map[string]any{"data": visualtest.SolidPNGBase64(4, 4, visualtest.Opaque), "width": 4, "height": 4},
	}}
	opts := stubOptions(t, godot)
	opts.BaselineDir = filepath.Join(t.TempDir(), "baselines")

	sc := mustParse(t, `{
		"name": "visual",
		"target": {"mode": "launch", "project_path": "proj", "headless": false},
		"steps": [
			{"name": "menu frame", "action": "screenshot", "with": {"output": "menu.png"}},
			{"action": "save_baseline", "with": {"name": "menu"}},
			{"action": "screenshot_diff", "with": {"name": "menu"}}
		]
	}`)

	report, err := Run(context.Background(), sc, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !report.Passed() {
		t.Fatalf("visual scenario failed: %+v", report.Failure)
	}

	for _, path := range []string{
		filepath.Join(opts.OutDir, reportFile),
		filepath.Join(opts.OutDir, junitFile),
		filepath.Join(opts.OutDir, traceFile),
		filepath.Join(opts.OutDir, "screenshots", "menu.png"),
		filepath.Join(opts.BaselineDir, "menu.png"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing artifact %s: %v", path, err)
		}
	}
	for _, key := range []string{"report", "junit", "rpc_trace"} {
		if report.Artifacts[key] == "" {
			t.Errorf("report.Artifacts is missing %q: %v", key, report.Artifacts)
		}
	}
	if report.RPC.Count != 3 {
		t.Errorf("rpc count = %d, want 3", report.RPC.Count)
	}
	for _, call := range report.RPC.Calls {
		if call.Method != "screenshot" {
			t.Errorf("traced method = %q, want screenshot", call.Method)
		}
		if call.StartedAt == "" {
			t.Error("traced call has no start timestamp")
		}
	}
}

func TestRunWritesDiffArtifactsOnVisualRegression(t *testing.T) {
	baselineDir := t.TempDir()
	if _, err := visual.SaveBaseline(baselineDir, "menu", "", visual.Shot{
		PNG:   visualtest.SolidPNG(4, 4, visualtest.Opaque),
		Width: 4, Height: 4,
	}); err != nil {
		t.Fatalf("SaveBaseline: %v", err)
	}

	godot := &stubGodot{results: map[string]any{
		"screenshot": map[string]any{"data": visualtest.SolidPNGBase64(4, 4, visualtest.Red), "width": 4, "height": 4},
	}}
	opts := stubOptions(t, godot)
	opts.BaselineDir = baselineDir

	sc := mustParse(t, `{
		"name": "regression",
		"target": {"mode": "launch", "project_path": "proj", "headless": false},
		"steps": [{"action": "screenshot_diff", "with": {"name": "menu"}}]
	}`)

	report, err := Run(context.Background(), sc, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Failure == nil || report.Failure.Kind != KindAssertion {
		t.Fatalf("failure = %+v, want an assertion failure", report.Failure)
	}
	if len(report.Steps[0].Artifacts) != 2 {
		t.Fatalf("step artifacts = %v, want the actual frame and the diff image", report.Steps[0].Artifacts)
	}
	for _, path := range report.Steps[0].Artifacts {
		if !strings.HasPrefix(path, filepath.Join(opts.OutDir, "diffs")) {
			t.Errorf("diff artifact %q escaped the run's diff directory", path)
		}
		if _, err := os.Stat(path); err != nil {
			t.Errorf("diff artifact %s not written: %v", path, err)
		}
	}
}

func TestRunFailsWhenPerformanceAssertionDoesNotHold(t *testing.T) {
	godot := &stubGodot{results: map[string]any{
		"assert_performance": map[string]any{
			"passed": false, "monitor": "TIME_FPS", "value": 22.0, "threshold": 55.0, "op": "gte",
		},
	}}
	sc := mustParse(t, `{
		"name": "perf",
		"target": {"mode": "launch", "project_path": "proj"},
		"steps": [{"action": "assert_performance", "with": {"monitor": "TIME_FPS", "threshold": 55, "op": "gte"}}]
	}`)

	report, err := Run(context.Background(), sc, stubOptions(t, godot))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Failure == nil || report.Failure.Kind != KindAssertion {
		t.Fatalf("failure = %+v, want an assertion failure", report.Failure)
	}
	if !strings.Contains(report.Steps[0].Error, "TIME_FPS") {
		t.Errorf("failure %q does not name the monitor", report.Steps[0].Error)
	}
}

func TestRunClassifiesTimeout(t *testing.T) {
	godot := &stubGodot{errs: map[string]error{"wait_for_node": context.DeadlineExceeded}}
	sc := mustParse(t, `{
		"name": "timeout",
		"target": {"mode": "launch", "project_path": "proj"},
		"steps": [{"action": "wait_for_node", "with": {"selector": "name:Never", "timeout_ms": 20}}]
	}`)

	report, err := Run(context.Background(), sc, stubOptions(t, godot))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Failure == nil || report.Failure.Kind != KindTimeout {
		t.Fatalf("failure = %+v, want a timeout failure", report.Failure)
	}
}

func TestJUnitSeparatesAssertionFailuresFromHarnessErrors(t *testing.T) {
	report := &Report{
		Name: "mixed", Status: StatusFailed, DurationMs: 1200,
		Steps: []StepResult{
			{Index: 0, Name: "ok step", Action: "click", Status: StatusPassed, DurationMs: 30},
			{Index: 1, Name: "bad value", Action: "assert_property", Status: StatusFailed, DurationMs: 40,
				Error: "text = \"Loading\", want equals \"Ready\"", ErrorKind: KindAssertion},
			{Index: 2, Name: "lost peer", Action: "tree", Status: StatusFailed, DurationMs: 5,
				Error: "write: broken pipe", ErrorKind: KindConnection},
			{Index: 3, Name: "never ran", Action: "tree", Status: StatusSkipped},
		},
	}
	path := filepath.Join(t.TempDir(), "junit.xml")
	if err := report.WriteJUnit(path); err != nil {
		t.Fatalf("WriteJUnit: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read junit: %v", err)
	}
	var doc junitSuites
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("junit is not valid XML: %v", err)
	}
	if len(doc.Suites) != 1 {
		t.Fatalf("suites = %d, want 1", len(doc.Suites))
	}
	suite := doc.Suites[0]
	if suite.Tests != 4 || suite.Failures != 1 || suite.Errors != 1 || suite.Skipped != 1 {
		t.Errorf("counts tests/failures/errors/skipped = %d/%d/%d/%d, want 4/1/1/1",
			suite.Tests, suite.Failures, suite.Errors, suite.Skipped)
	}
	if suite.Cases[1].Failure == nil {
		t.Error("an assertion failure must be a <failure>")
	}
	if suite.Cases[2].Error == nil {
		t.Error("a connection failure must be an <error>, not a <failure>")
	}
	if suite.Cases[3].Skipped == nil {
		t.Error("a skipped step must be <skipped>")
	}
}

func TestSleepStepDoesNotCallGodot(t *testing.T) {
	godot := &stubGodot{}
	sc := mustParse(t, `{
		"name": "sleep",
		"target": {"mode": "launch", "project_path": "proj"},
		"steps": [{"action": "sleep", "with": {"duration_ms": 1}}]
	}`)
	opts := stubOptions(t, godot)
	opts.now = time.Now

	report, err := Run(context.Background(), sc, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !report.Passed() {
		t.Fatalf("sleep step failed: %+v", report.Failure)
	}
	if len(godot.called()) != 0 {
		t.Errorf("sleep issued RPCs: %v", godot.called())
	}
}
