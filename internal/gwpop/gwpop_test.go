package gwpop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mrf/godot-stagehand/internal/godotconn"
)

// recordingCaller answers every call with a canned result and records what it
// was asked to send.
type recordingCaller struct {
	method string
	params any
	result json.RawMessage
	err    error
	calls  int
}

func (r *recordingCaller) Call(_ context.Context, method string, params any) (*godotconn.Response, error) {
	r.calls++
	r.method = method
	r.params = params
	if r.err != nil {
		return nil, r.err
	}
	result := r.result
	if result == nil {
		result = json.RawMessage(`{}`)
	}
	return &godotconn.Response{JSONRPC: "2.0", ID: 1, Result: result}, nil
}

func paramsMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("params type = %T, want map[string]any", v)
	}
	return m
}

func TestExecuteMapsActionToProtocolMethod(t *testing.T) {
	cases := []struct {
		action string
		params map[string]any
		method string
	}{
		{"tree", nil, "get_tree"},
		{"find", map[string]any{"selector": "class:Button"}, "query_nodes"},
		{"click", map[string]any{"selector": "class:Button"}, "input_mouse"},
		{"press_key", map[string]any{"key": "Enter"}, "input_key"},
		{"focus_window", nil, "focus_window"},
		{"press_action", map[string]any{"action": "ui_accept"}, "input_action"},
		{"type_text", map[string]any{"text": "hi"}, "input_text"},
		{"wait_for_signal", map[string]any{"selector": "name:X", "signal_name": "done"}, "wait_signal"},
		{"get_performance", nil, "get_performance"},
	}
	for _, testCase := range cases {
		t.Run(testCase.action, func(t *testing.T) {
			caller := &recordingCaller{}
			if _, err := Execute(context.Background(), caller, Op{Action: testCase.action, Params: testCase.params}); err != nil {
				t.Fatalf("Execute(%s): %v", testCase.action, err)
			}
			if caller.method != testCase.method {
				t.Errorf("method = %q, want %q", caller.method, testCase.method)
			}
		})
	}
}

func TestExecuteRejectsUnknownAction(t *testing.T) {
	caller := &recordingCaller{}
	_, err := Execute(context.Background(), caller, Op{Action: "teleport"})
	if err == nil {
		t.Fatal("Execute accepted an unknown action")
	}
	if KindOf(err) != KindUsage {
		t.Errorf("KindOf = %v, want KindUsage", KindOf(err))
	}
	if caller.calls != 0 {
		t.Error("an unknown action must not reach the wire")
	}
}

func TestExecuteRejectsMissingRequiredParam(t *testing.T) {
	caller := &recordingCaller{}
	_, err := Execute(context.Background(), caller, Op{Action: "get_property", Params: map[string]any{"selector": "name:X"}})
	if err == nil {
		t.Fatal("Execute accepted get_property without a property")
	}
	if KindOf(err) != KindUsage {
		t.Errorf("KindOf = %v, want KindUsage", KindOf(err))
	}
	if !strings.Contains(err.Error(), "property") {
		t.Errorf("error %q does not name the missing parameter", err)
	}
	if caller.calls != 0 {
		t.Error("a missing required parameter must not reach the wire")
	}
}

func TestExecuteRejectsUnknownParam(t *testing.T) {
	caller := &recordingCaller{}
	_, err := Execute(context.Background(), caller, Op{
		Action: "find",
		Params: map[string]any{"selector": "class:Button", "limitt": 5},
	})
	if err == nil {
		t.Fatal("Execute accepted a misspelled parameter")
	}
	if KindOf(err) != KindUsage {
		t.Errorf("KindOf = %v, want KindUsage", KindOf(err))
	}
	if caller.calls != 0 {
		t.Error("an unknown parameter must not reach the wire")
	}
}

func TestExecuteValidatesSelectorsLocally(t *testing.T) {
	caller := &recordingCaller{}
	_, err := Execute(context.Background(), caller, Op{
		Action: "find",
		Params: map[string]any{"selector": "class:"},
	})
	if err == nil {
		t.Fatal("Execute accepted an invalid selector")
	}
	if caller.calls != 0 {
		t.Error("an invalid selector must be rejected before the wire")
	}
}

func TestExecuteRequiresOneOfGroup(t *testing.T) {
	caller := &recordingCaller{}
	if _, err := Execute(context.Background(), caller, Op{Action: "click"}); err == nil {
		t.Fatal("click accepted neither a selector nor a position")
	}
	if caller.calls != 0 {
		t.Error("click without a target must not reach the wire")
	}

	ok := &recordingCaller{}
	if _, err := Execute(context.Background(), ok, Op{
		Action: "click",
		Params: map[string]any{"position": map[string]any{"x": 1.0, "y": 2.0}},
	}); err != nil {
		t.Fatalf("click by position: %v", err)
	}
	if _, present := paramsMap(t, ok.params)["position"]; !present {
		t.Error("position was not forwarded")
	}
}

func TestExecuteBlocksDestructiveMethodCalls(t *testing.T) {
	for _, method := range []string{"queue_free", "free", "_ready"} {
		caller := &recordingCaller{}
		_, err := Execute(context.Background(), caller, Op{
			Action: "call_method",
			Params: map[string]any{"selector": "name:X", "method": method},
		})
		if err == nil {
			t.Errorf("call_method accepted blocked method %q", method)
		}
		if caller.calls != 0 {
			t.Errorf("blocked method %q reached the wire", method)
		}
	}
}

func TestExecuteSurfacesAddonReportedError(t *testing.T) {
	caller := &recordingCaller{result: json.RawMessage(`{"error":"Node not found for selector: name:X","error_code":"no_match"}`)}
	_, err := Execute(context.Background(), caller, Op{Action: "find", Params: map[string]any{"selector": "name:X"}})
	if err == nil {
		t.Fatal("Execute ignored an addon-reported error inside a successful result")
	}
	if KindOf(err) != KindRemote {
		t.Errorf("KindOf = %v, want KindRemote", KindOf(err))
	}
	if !strings.Contains(err.Error(), "no_match") {
		t.Errorf("error %q does not carry the addon error code", err)
	}
}

func TestExecuteClassifiesTransportFailure(t *testing.T) {
	caller := &recordingCaller{err: errors.New("write: broken pipe")}
	_, err := Execute(context.Background(), caller, Op{Action: "tree"})
	if KindOf(err) != KindTransport {
		t.Errorf("KindOf = %v, want KindTransport", KindOf(err))
	}
}

func TestExecuteClassifiesTimeout(t *testing.T) {
	caller := &recordingCaller{err: context.DeadlineExceeded}
	_, err := Execute(context.Background(), caller, Op{
		Action: "wait_for_node",
		Params: map[string]any{"selector": "name:X", "timeout_ms": 5},
	})
	if KindOf(err) != KindTimeout {
		t.Errorf("KindOf = %v, want KindTimeout", KindOf(err))
	}
}

func TestExecuteAcceptsPerformanceSamplingParams(t *testing.T) {
	caller := &recordingCaller{}
	_, err := Execute(context.Background(), caller, Op{
		Action: "assert_performance",
		Params: map[string]any{
			"monitor": "TIME_FPS", "threshold": 55.0, "op": "gte",
			"statistic": "p95", "warmup_ms": 100, "sample_count": 30, "sample_interval_ms": 16,
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	params := paramsMap(t, caller.params)
	for _, key := range []string{"statistic", "warmup_ms", "sample_count", "sample_interval_ms"} {
		if _, present := params[key]; !present {
			t.Errorf("%s was not forwarded", key)
		}
	}
}

func TestExecuteAcceptsDurationMsForPerformanceSampling(t *testing.T) {
	caller := &recordingCaller{}
	_, err := Execute(context.Background(), caller, Op{
		Action: "assert_performance",
		Params: map[string]any{
			"monitor": "TIME_FPS", "threshold": 55.0, "duration_ms": 500,
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, present := paramsMap(t, caller.params)["duration_ms"]; !present {
		t.Error("duration_ms was not forwarded")
	}
}

// focus_window takes no required parameters — its whole point is that a stuck
// caller can invoke it with nothing but the connection and have the addon pick
// the modal that lost focus (godot-stagehand-z6iu). Its one optional parameter
// is a selector, so it must be validated like every other selector.
func TestFocusWindowSelectorIsValidated(t *testing.T) {
	caller := &recordingCaller{}
	_, err := Execute(context.Background(), caller, Op{
		Action: "focus_window",
		Params: map[string]any{"selector": "name:"},
	})
	if err == nil {
		t.Fatal("Execute accepted a malformed selector")
	}
	if KindOf(err) != KindUsage {
		t.Errorf("KindOf = %v, want KindUsage", KindOf(err))
	}
	if caller.calls != 0 {
		t.Error("a malformed selector must not reach the wire")
	}

	caller = &recordingCaller{}
	if _, err := Execute(context.Background(), caller, Op{
		Action: "focus_window",
		Params: map[string]any{"selector": "class:AcceptDialog"},
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := paramsMap(t, caller.params)["selector"]; got != "class:AcceptDialog" {
		t.Errorf("selector = %v, want class:AcceptDialog", got)
	}
}

func TestActionsAreSortedAndCoverThePublicSurface(t *testing.T) {
	actions := Actions()
	for _, want := range []string{
		"tree", "find", "get_property", "set_property", "call_method",
		"click", "press_key", "focus_window", "wait_for_node", "screenshot",
		"get_performance",
	} {
		found := false
		for _, action := range actions {
			if action == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Actions() is missing %q", want)
		}
	}
	for i := 1; i < len(actions); i++ {
		if actions[i-1] >= actions[i] {
			t.Fatalf("Actions() is not sorted: %q before %q", actions[i-1], actions[i])
		}
	}
}

func TestCompareOperators(t *testing.T) {
	cases := []struct {
		operator string
		actual   any
		expected any
		want     bool
	}{
		{"equals", "hello", "hello", true},
		{"equals", 3.0, 3.0, true},
		{"equals", 3.0, 4.0, false},
		{"not_equals", "a", "b", true},
		{"contains", "hello world", "world", true},
		{"contains", "hello", "bye", false},
		{"contains", []any{"a", "b"}, "b", true},
		{"greater_than", 5.0, 3.0, true},
		{"greater_than", 2.0, 3.0, false},
		{"less_than", 2.0, 3.0, true},
		{"exists", "anything", nil, true},
		{"exists", nil, nil, false},
	}
	for _, testCase := range cases {
		got, err := Compare(testCase.operator, testCase.actual, testCase.expected)
		if err != nil {
			t.Fatalf("Compare(%s): %v", testCase.operator, err)
		}
		if got != testCase.want {
			t.Errorf("Compare(%s, %v, %v) = %v, want %v", testCase.operator, testCase.actual, testCase.expected, got, testCase.want)
		}
	}
}

func TestCompareRejectsUnknownOperator(t *testing.T) {
	if _, err := Compare("approximately", 1.0, 1.0); err == nil {
		t.Fatal("Compare accepted an unknown operator")
	}
}

func TestCompareRejectsNonNumericOrdering(t *testing.T) {
	if _, err := Compare("greater_than", "abc", 3.0); err == nil {
		t.Fatal("Compare accepted an ordering comparison on non-numbers")
	}
}

// A JSON-RPC error object is a *reply*, not a broken connection. Classifying it
// as KindTransport gave the CLI and scenario runner the wrong exit code for
// every addon-reported failure once the addon began promoting handler failures
// to JSON-RPC errors (godot-stagehand-vv2.8).
func TestExecuteClassifiesJSONRPCError(t *testing.T) {
	tests := []struct {
		name     string
		code     int
		wantKind Kind
	}{
		{"target not found is a remote failure", godotconn.CodeTargetNotFound, KindRemote},
		{"handler error is a remote failure", godotconn.CodeHandlerError, KindRemote},
		{"internal error is a remote failure", godotconn.CodeInternalError, KindRemote},
		{"invalid params is a caller mistake", godotconn.CodeInvalidParams, KindUsage},
		{"unknown method is a caller mistake", godotconn.CodeMethodNotFound, KindUsage},
		{"addon-side timeout is a timeout", godotconn.CodeTimeout, KindTimeout},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			caller := &recordingCaller{err: &godotconn.RPCError{
				Code:    tc.code,
				Message: "Node not found for selector: name:X",
				Data: json.RawMessage(
					`{"error_code":"node_not_found","method":"query_nodes","selector":"name:X",` +
						`"details":{"next_action":"Call get_tree to confirm the node exists."}}`),
			}}
			_, err := Execute(context.Background(), caller, Op{
				Action: "find", Params: map[string]any{"selector": "name:X"},
			})
			if err == nil {
				t.Fatal("Execute ignored a JSON-RPC error response")
			}
			if KindOf(err) != tc.wantKind {
				t.Errorf("KindOf = %v, want %v", KindOf(err), tc.wantKind)
			}
			// The rendered message must carry the machine kind and the
			// remediation hint, not just the bare message.
			for _, want := range []string{"query_nodes failed", "code=node_not_found", "Call get_tree"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q is missing %q", err, want)
				}
			}
		})
	}
}
