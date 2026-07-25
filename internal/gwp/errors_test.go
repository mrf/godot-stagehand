package gwp

import "testing"

func TestRemoteFailureDescribe(t *testing.T) {
	tests := []struct {
		name    string
		failure RemoteFailure
		want    string
	}{
		{
			name: "full envelope promotes next_action and renders the rest",
			failure: RemoteFailure{
				Method:   "get_property",
				Selector: "name:Ghost",
				Code:     "node_not_found",
				Message:  "Node not found for selector: name:Ghost",
				Details: map[string]any{
					"next_action": "Call get_tree to confirm the node exists.",
					"selector":    "name:Ghost",
				},
			},
			want: `get_property failed: Node not found for selector: name:Ghost ` +
				`(code=node_not_found, selector="name:Ghost")` +
				` — Call get_tree to confirm the node exists. [selector=name:Ghost]`,
		},
		{
			name: "details render sorted with compact numbers",
			failure: RemoteFailure{
				Method:  "wait_for_node",
				Code:    "timeout",
				Message: "Node did not appear before timeout",
				Details: map[string]any{
					"timeout_ms": float64(2500),
					"state":      "exists",
					"retried":    false,
				},
			},
			want: "wait_for_node failed: Node did not appear before timeout (code=timeout) " +
				"[retried=false, state=exists, timeout_ms=2500]",
		},
		{
			name: "bare message from an addon with no structured data",
			failure: RemoteFailure{
				Method:  "get_property",
				Message: "Method not found: get_property",
			},
			want: "get_property failed: Method not found: get_property",
		},
		{
			// A protocol-level fault (parse error) names no method, so there is
			// nothing to attribute the failure to.
			name:    "no method and no message still renders something reportable",
			failure: RemoteFailure{},
			want:    "Godot reported a handler failure with no message",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.failure.Describe(); got != tc.want {
				t.Errorf("Describe()\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

// A composite details value must not break the rendering — it falls back to
// JSON rather than Go's %v, so the output stays machine-readable.
func TestRemoteFailureDescribeCompositeDetail(t *testing.T) {
	failure := RemoteFailure{
		Method:  "assert_performance",
		Code:    "invalid_params",
		Message: "Unknown monitor: FPS",
		Details: map[string]any{"known_monitors": []any{"TIME_FPS", "OBJECT_COUNT"}},
	}
	want := `assert_performance failed: Unknown monitor: FPS (code=invalid_params) ` +
		`[known_monitors=["TIME_FPS","OBJECT_COUNT"]]`
	if got := failure.Describe(); got != want {
		t.Errorf("Describe()\n got: %s\nwant: %s", got, want)
	}
}

// LegacyFailure is the compatibility path for an addon vendored into a host
// project before the canonical error model. Getting it wrong would hand a
// failure to the caller as a success, so pin every branch.
func TestLegacyFailure(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantOK      bool
		wantMessage string
		wantCode    string
	}{
		{
			name:        "full triple",
			raw:         `{"error":"Node not found","error_code":"no_match","details":{"selector":"name:X"}}`,
			wantOK:      true,
			wantMessage: "Node not found",
			wantCode:    "no_match",
		},
		{
			name:        "message only",
			raw:         `{"error":"Missing selector"}`,
			wantOK:      true,
			wantMessage: "Missing selector",
		},
		{
			// An unreadable failure must still be reported as a failure — the
			// alternative is silently treating it as a success.
			name:        "non-string error value",
			raw:         `{"error":{"nested":true}}`,
			wantOK:      true,
			wantMessage: "godot handler error (unparseable)",
		},
		{name: "successful result", raw: `{"value":42,"type":"int"}`, wantOK: false},
		{name: "empty result", raw: `{}`, wantOK: false},
		{name: "not an object", raw: `[1,2,3]`, wantOK: false},
		{name: "not json", raw: `<html>`, wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			failure, ok := LegacyFailure([]byte(tc.raw))
			if ok != tc.wantOK {
				t.Fatalf("LegacyFailure(%s) ok = %v, want %v", tc.raw, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if failure.Message != tc.wantMessage {
				t.Errorf("Message = %q, want %q", failure.Message, tc.wantMessage)
			}
			if failure.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", failure.Code, tc.wantCode)
			}
			// A legacy result never names the method that failed.
			if failure.Method != "" {
				t.Errorf("Method = %q, want empty", failure.Method)
			}
		})
	}
}
