package scenario

import (
	"context"
	"testing"

	"github.com/mrf/godot-stagehand/internal/godotconn"
)

// stubCaller answers every RPC with an empty result, standing in for Godot.
type stubCaller struct{}

func (stubCaller) Call(context.Context, string, any) (*godotconn.Response, error) {
	return &godotconn.Response{JSONRPC: "2.0", ID: 1}, nil
}

// TestTracerDistinguishesPhaseAtSameStepIndex reproduces godot-stagehand-10nn:
// the step phase's index 0 and the teardown phase's index 0 must not collapse
// into indistinguishable trace entries.
func TestTracerDistinguishesPhaseAtSameStepIndex(t *testing.T) {
	tr := newTracer(stubCaller{}, nil)

	tr.setStep("step", 0)
	if _, err := tr.Call(context.Background(), "set_property", nil); err != nil {
		t.Fatalf("Call: %v", err)
	}
	tr.setStep("teardown", 0)
	if _, err := tr.Call(context.Background(), "set_property", nil); err != nil {
		t.Fatalf("Call: %v", err)
	}

	calls := tr.Trace().Calls
	if len(calls) != 2 {
		t.Fatalf("recorded %d calls, want 2", len(calls))
	}
	if calls[0].Phase != "step" || calls[0].Step != 0 {
		t.Errorf("call 0 = %+v, want phase=step step=0", calls[0])
	}
	if calls[1].Phase != "teardown" || calls[1].Step != 0 {
		t.Errorf("call 1 = %+v, want phase=teardown step=0, otherwise indistinguishable from call 0", calls[1])
	}
}
