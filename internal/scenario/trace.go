package scenario

import (
	"context"
	"sync"
	"time"

	"github.com/mrf/godot-stagehand/internal/godotconn"
	"github.com/mrf/godot-stagehand/internal/gwpop"
)

// TraceCall is one recorded RPC. Timings are measured around the wire call
// only, so a slow step is attributable to Godot rather than to the runner.
type TraceCall struct {
	Seq        int    `json:"seq"`
	Step       int    `json:"step"`
	Method     string `json:"method"`
	StartedAt  string `json:"started_at"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// Trace is the RPC timing record emitted alongside the run report.
type Trace struct {
	Count           int         `json:"count"`
	TotalDurationMs int64       `json:"total_duration_ms"`
	SlowestMethod   string      `json:"slowest_method,omitempty"`
	SlowestMs       int64       `json:"slowest_duration_ms,omitempty"`
	Calls           []TraceCall `json:"calls"`
}

// tracer decorates a Caller, timing every RPC and attributing it to the step
// that is currently executing.
type tracer struct {
	inner gwpop.Caller
	now   func() time.Time

	mu    sync.Mutex
	step  int
	calls []TraceCall
}

func newTracer(inner gwpop.Caller, now func() time.Time) *tracer {
	if now == nil {
		now = time.Now
	}
	return &tracer{inner: inner, now: now, step: -1}
}

// setStep attributes subsequent calls to a step index.
func (t *tracer) setStep(index int) {
	t.mu.Lock()
	t.step = index
	t.mu.Unlock()
}

func (t *tracer) Call(ctx context.Context, method string, params any) (*godotconn.Response, error) {
	start := t.now()
	resp, err := t.inner.Call(ctx, method, params)
	elapsed := t.now().Sub(start)

	t.mu.Lock()
	call := TraceCall{
		Seq:        len(t.calls) + 1,
		Step:       t.step,
		Method:     method,
		StartedAt:  start.UTC().Format(time.RFC3339Nano),
		DurationMs: elapsed.Milliseconds(),
	}
	if err != nil {
		call.Error = err.Error()
	}
	t.calls = append(t.calls, call)
	t.mu.Unlock()

	return resp, err
}

// Trace snapshots the recorded calls.
func (t *tracer) Trace() Trace {
	t.mu.Lock()
	defer t.mu.Unlock()

	trace := Trace{Count: len(t.calls), Calls: append([]TraceCall{}, t.calls...)}
	for _, call := range t.calls {
		trace.TotalDurationMs += call.DurationMs
		if call.DurationMs > trace.SlowestMs {
			trace.SlowestMs = call.DurationMs
			trace.SlowestMethod = call.Method
		}
	}
	return trace
}
