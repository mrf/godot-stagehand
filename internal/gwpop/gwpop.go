// Package gwpop is the protocol-agnostic operation registry for the Godot Wire
// Protocol. It maps a stable action name (the vocabulary the CLI and the
// scenario runner expose) onto a GWP method plus its validated parameters, so
// both frontends accept exactly the same operations and reject the same
// mistakes before anything reaches the wire.
//
// The MCP server deliberately does not route through this registry: its tool
// schemas are the contract there, and mcp-go already validates against them.
// What the two surfaces do share lives in narrower packages (internal/visual,
// internal/selector, this package's method blocklist).
package gwpop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mrf/godot-stagehand/internal/godotconn"
	"github.com/mrf/godot-stagehand/internal/gwp"
	"github.com/mrf/godot-stagehand/internal/selector"
)

// Caller performs one GWP JSON-RPC call. *godotconn.Connection satisfies it;
// the scenario runner wraps that in a tracing decorator.
type Caller interface {
	Call(ctx context.Context, method string, params any) (*godotconn.Response, error)
}

// Op is one operation: a stable action name plus its parameters, however the
// frontend obtained them (CLI flags, or a step in a scenario file).
type Op struct {
	Action string
	Params map[string]any
}

// Kind classifies a failure so a frontend can map it onto a stable exit code
// without string-matching.
type Kind int

const (
	// KindUsage is a caller mistake: unknown action, missing or unknown
	// parameter, malformed selector. Nothing was sent.
	KindUsage Kind = iota
	// KindTransport is a connection-level failure.
	KindTransport
	// KindRemote is an error the addon reported for a well-formed request.
	KindRemote
	// KindTimeout is a deadline expiring before Godot answered.
	KindTimeout
)

func (k Kind) String() string {
	switch k {
	case KindUsage:
		return "usage"
	case KindTransport:
		return "transport"
	case KindRemote:
		return "remote"
	case KindTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}

// Error carries the failure kind alongside the message.
type Error struct {
	Kind    Kind
	Action  string
	Message string
}

func (e *Error) Error() string {
	if e.Action == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Action, e.Message)
}

// KindOf reports the classification of err, defaulting to KindTransport for
// errors that did not originate here (an unwrapped connection failure).
func KindOf(err error) Kind {
	var opErr *Error
	if errors.As(err, &opErr) {
		return opErr.Kind
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return KindTimeout
	}
	return KindTransport
}

func newError(kind Kind, action, format string, args ...any) *Error {
	return &Error{Kind: kind, Action: action, Message: fmt.Sprintf(format, args...)}
}

// Spec describes one action's contract.
type Spec struct {
	// Action is the stable name used in scenario files and CLI commands.
	Action string
	// Method is the GWP method the action maps onto.
	Method string
	// Required parameters; absence is a usage error.
	Required []string
	// Optional parameters; forwarded verbatim when present.
	Optional []string
	// OneOf groups: at least one member of each group must be supplied.
	OneOf [][]string
	// Selectors names the parameters validated with selector.ParseChain.
	Selectors []string
	// TimeoutParam, when set, names the millisecond parameter that bounds the
	// remote operation. The Go-side deadline is that value plus DeadlineBuffer.
	TimeoutParam string
	// DefaultTimeoutMs applies when TimeoutParam is set but not supplied.
	DefaultTimeoutMs int
	// Capability is the GWP capability the addon must advertise.
	Capability string
	// Summary is the one-line description shown in CLI help.
	Summary string
}

// DeadlineBuffer is added to a wait action's timeout_ms to compute the Go-side
// context deadline, giving Godot time to detect its own timeout and answer.
// Without it, an alive-but-frozen game (TCP still open, _process not ticking)
// would hang the caller indefinitely.
const DeadlineBuffer = 2 * time.Second

var specs = buildSpecs()

func buildSpecs() map[string]Spec {
	list := []Spec{
		{Action: "ping", Method: "ping", Capability: gwp.CapabilityCore,
			Summary: "Handshake with the addon and report engine/protocol info"},
		{Action: "game_state", Method: "get_game_state", Capability: gwp.CapabilityCore,
			Summary: "Report the current scene, FPS, physics state and window size"},
		{Action: "tree", Method: "get_tree", Capability: gwp.CapabilityCore,
			Optional: []string{"root_path", "max_depth", "properties"}, Selectors: []string{"root_path"},
			Summary: "Snapshot the scene tree"},
		{Action: "find", Method: "query_nodes", Capability: gwp.CapabilityCore,
			Required: []string{"selector"}, Optional: []string{"properties", "limit"}, Selectors: []string{"selector"},
			Summary: "Find nodes matching a selector"},
		{Action: "get_property", Method: "get_property", Capability: gwp.CapabilityCore,
			Required: []string{"selector", "property"}, Selectors: []string{"selector"},
			Summary: "Read a node property"},
		{Action: "set_property", Method: "set_property", Capability: gwp.CapabilityCore,
			Required: []string{"selector", "property", "value"}, Selectors: []string{"selector"},
			Summary: "Write a node property"},
		{Action: "call_method", Method: "call_method", Capability: gwp.CapabilityUnsafe,
			Required: []string{"selector", "method"}, Optional: []string{"args", "allow_multiple"}, Selectors: []string{"selector"},
			Summary: "Call a method on a node (destructive and private methods are blocked)"},
		{Action: "evaluate", Method: "evaluate", Capability: gwp.CapabilityUnsafe,
			Required: []string{"expression"}, Optional: []string{"context_node"}, Selectors: []string{"context_node"},
			Summary: "Evaluate a GDScript expression"},
		{Action: "change_scene", Method: "change_scene", Capability: gwp.CapabilityCore,
			Required: []string{"scene_path"},
			Summary:  "Change to a different scene"},

		{Action: "click", Method: "input_mouse", Capability: gwp.CapabilityInput,
			OneOf: [][]string{{"selector", "position"}}, Optional: []string{"selector", "position", "button", "double_click"}, Selectors: []string{"selector"},
			Summary: "Click a node or screen coordinates"},
		{Action: "press_key", Method: "input_key", Capability: gwp.CapabilityInput,
			Required: []string{"key"}, Optional: []string{"modifiers", "hold_ms"},
			Summary: "Press a keyboard key"},
		{Action: "focus_window", Method: "focus_window", Capability: gwp.CapabilityInput,
			Optional: []string{"selector"}, Selectors: []string{"selector"},
			Summary: "Give a Window focus so key input reaches it (defaults to the modal that lost focus)"},
		{Action: "press_action", Method: "input_action", Capability: gwp.CapabilityInput,
			Required: []string{"action"}, Optional: []string{"strength", "hold_ms"},
			Summary: "Trigger a Godot input action"},
		{Action: "type_text", Method: "input_text", Capability: gwp.CapabilityInput,
			Required: []string{"text"}, Optional: []string{"delay_ms", "selector"}, Selectors: []string{"selector"},
			Summary: "Type text into the focused control"},
		{Action: "mouse_move", Method: "input_mouse_move", Capability: gwp.CapabilityInput,
			OneOf: [][]string{{"selector", "coordinates"}}, Optional: []string{"selector", "coordinates"}, Selectors: []string{"selector"},
			Summary: "Move the mouse cursor without clicking"},
		{Action: "touch", Method: "input_touch", Capability: gwp.CapabilityInput,
			Required: []string{"position"}, Optional: []string{"index", "action", "drag_to", "duration_ms"},
			Summary: "Simulate a touch or drag"},

		{Action: "wait_for_node", Method: "wait_for_node", Capability: gwp.CapabilityWait,
			Required: []string{"selector"}, Optional: []string{"state", "timeout_ms", "poll_interval_ms"}, Selectors: []string{"selector"},
			TimeoutParam: "timeout_ms", DefaultTimeoutMs: 10000,
			Summary: "Wait for a node to exist, become visible, or be removed"},
		{Action: "wait_for_signal", Method: "wait_signal", Capability: gwp.CapabilityWait,
			Required: []string{"selector", "signal_name"}, Optional: []string{"timeout_ms"}, Selectors: []string{"selector"},
			TimeoutParam: "timeout_ms", DefaultTimeoutMs: 5000,
			Summary: "Wait for a signal emission"},
		{Action: "wait_for_property", Method: "wait_for_property", Capability: gwp.CapabilityWait,
			Required: []string{"selector", "property", "operator"}, Optional: []string{"expected_value", "timeout_ms", "poll_interval_ms"}, Selectors: []string{"selector"},
			TimeoutParam: "timeout_ms", DefaultTimeoutMs: 10000,
			Summary: "Wait for a property to satisfy a condition"},

		// "screenshot" is registered here only so Capture (which always calls
		// Execute with action "screenshot") resolves a spec; the scenario
		// runner's localSpecs entry is the one SpecFor actually exposes to
		// scenario/CLI callers. Optional is deliberately just "selector":
		// Capture never forwards a "full_page" param on any path, so
		// advertising it here would be a dead, unreachable knob. full_page is
		// a real, wired option only on the MCP server surface
		// (mcpserver/tools_visual.go), which does not use this registry.
		{Action: "screenshot", Method: "screenshot", Capability: gwp.CapabilityScreenshot,
			Optional: []string{"selector"}, Selectors: []string{"selector"},
			Summary: "Capture the viewport"},

		{Action: "get_performance", Method: "get_performance", Capability: gwp.CapabilityPerformance,
			Optional: []string{"monitors"},
			Summary:  "Read Performance singleton monitors"},
		{Action: "assert_performance", Method: "assert_performance", Capability: gwp.CapabilityPerformance,
			Required: []string{"monitor", "threshold"},
			Optional: []string{
				"op", "statistic", "warmup_ms", "sample_interval_ms", "sample_count", "duration_ms",
			},
			Summary: "Assert a performance monitor against a threshold"},

		{Action: "record_start", Method: "record_start", Capability: gwp.CapabilityRecording,
			Required: []string{"output_path"},
			Summary:  "Start recording input"},
		{Action: "record_stop", Method: "record_stop", Capability: gwp.CapabilityRecording,
			Summary: "Stop the active recording"},
		{Action: "replay", Method: "replay", Capability: gwp.CapabilityRecording,
			Required: []string{"input_path"},
			Summary:  "Replay a recorded input session"},
	}
	byName := make(map[string]Spec, len(list))
	for _, spec := range list {
		byName[spec.Action] = spec
	}
	return byName
}

// Lookup returns the spec for action.
func Lookup(action string) (Spec, bool) {
	spec, ok := specs[action]
	return spec, ok
}

// Actions returns every registered action name, sorted.
func Actions() []string {
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Execute validates op and performs the corresponding GWP call. The returned
// JSON is the addon's raw result; an addon-reported failure inside an otherwise
// successful result is surfaced as a KindRemote error.
func Execute(ctx context.Context, c Caller, op Op) (json.RawMessage, error) {
	spec, ok := Lookup(op.Action)
	if !ok {
		return nil, newError(KindUsage, "", "unknown action %q (known: %s)", op.Action, strings.Join(Actions(), ", "))
	}
	params, err := spec.Params(op.Params)
	if err != nil {
		return nil, err
	}

	callCtx := ctx
	if spec.TimeoutParam != "" {
		timeoutMs, err := spec.resolveTimeoutMs(params)
		if err != nil {
			return nil, err
		}
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond+DeadlineBuffer)
		defer cancel()
	}

	var wireParams any
	if len(params) > 0 {
		wireParams = params
	}
	resp, err := c.Call(callCtx, spec.Method, wireParams)
	if err != nil {
		var rpcErr *godotconn.RPCError
		if errors.As(err, &rpcErr) {
			return nil, rpcError(op.Action, spec.Method, rpcErr)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, newError(KindTimeout, op.Action, "timed out waiting for Godot to answer %q", spec.Method)
		}
		return nil, newError(KindTransport, op.Action, "%v", err)
	}
	if err := checkAddonError(op.Action, resp.Result); err != nil {
		return nil, err
	}
	return resp.Result, nil
}

// Params validates the supplied parameters against the spec and returns the
// map to put on the wire. Exported so the scenario runner can validate a whole
// file up front — before launching Godot — with the identical rules Execute
// applies at call time.
func (s Spec) Params(supplied map[string]any) (map[string]any, error) {
	allowed := s.acceptedParams(true)

	out := make(map[string]any, len(supplied))
	for name, value := range supplied {
		if value == nil {
			continue
		}
		if !slices.Contains(allowed, name) {
			return nil, newError(KindUsage, s.Action, "unknown parameter %q (accepted: %s)", name, strings.Join(allowed, ", "))
		}
		out[name] = value
	}

	for _, name := range s.Required {
		if _, ok := out[name]; !ok {
			return nil, newError(KindUsage, s.Action, "missing required parameter %q", name)
		}
	}
	for _, group := range s.OneOf {
		if !hasAny(out, group) {
			return nil, newError(KindUsage, s.Action, "one of %s is required", strings.Join(group, ", "))
		}
	}
	for _, name := range s.Selectors {
		raw, ok := out[name]
		if !ok {
			continue
		}
		text, ok := raw.(string)
		if !ok {
			return nil, newError(KindUsage, s.Action, "parameter %q must be a string selector", name)
		}
		if _, err := selector.ParseChain(text); err != nil {
			return nil, newError(KindUsage, s.Action, "invalid selector %q: %v", text, err)
		}
	}
	if s.Action == "call_method" {
		name, ok := out["method"].(string)
		if !ok {
			return nil, newError(KindUsage, s.Action, "parameter \"method\" must be a string")
		}
		if err := ValidateMethodName(name); err != nil {
			return nil, newError(KindUsage, s.Action, "%v", err)
		}
	}
	return out, nil
}

func (s Spec) resolveTimeoutMs(params map[string]any) (int, error) {
	raw, ok := params[s.TimeoutParam]
	if !ok {
		params[s.TimeoutParam] = s.DefaultTimeoutMs
		return s.DefaultTimeoutMs, nil
	}
	ms, ok := asInt(raw)
	if !ok || ms <= 0 {
		return 0, newError(KindUsage, s.Action, "parameter %q must be a positive number of milliseconds, got %v", s.TimeoutParam, raw)
	}
	params[s.TimeoutParam] = ms
	return ms, nil
}

// rpcError classifies a JSON-RPC error object from the addon. A JSON-RPC error
// is a *reply*, so it is never a transport failure — reporting it as one gave
// the CLI and scenario runner the wrong exit code for every handler failure
// once the addon began promoting those to JSON-RPC errors (godot-stagehand-vv2.8).
// The numeric code carries the classification; the addon's fine-grained kind and
// its remediation hint ride along in the rendered message.
func rpcError(action, method string, rpcErr *godotconn.RPCError) *Error {
	kind := KindRemote
	switch rpcErr.Code {
	case godotconn.CodeInvalidParams, godotconn.CodeInvalidRequest, godotconn.CodeMethodNotFound:
		// The addon rejected the request itself: a caller mistake, not a fault
		// in the running game.
		kind = KindUsage
	case godotconn.CodeTimeout:
		kind = KindTimeout
	}
	return newError(kind, action, "%s", rpcErr.Failure(method).Describe())
}

// checkAddonError surfaces the `{"error", "error_code", "details"}` triple an
// addon predating the canonical error model embeds in an otherwise successful
// JSON-RPC result. Current addons promote those failures to JSON-RPC errors,
// which rpcError handles instead.
func checkAddonError(action string, raw json.RawMessage) error {
	failure, ok := gwp.LegacyFailure(raw)
	if !ok {
		return nil
	}
	return newError(KindRemote, action, "%s", failure.Describe())
}

// AcceptedParams lists the optional parameters an action takes, deduplicated
// and in declaration order. The CLI documents the scenario vocabulary from it,
// so help output cannot drift from what Params actually accepts.
func (s Spec) AcceptedParams() []string { return s.acceptedParams(false) }

func (s Spec) acceptedParams(includeRequired bool) []string {
	all := make([]string, 0, len(s.Required)+len(s.Optional))
	if includeRequired {
		all = append(all, s.Required...)
	}
	all = append(all, s.Optional...)
	for _, group := range s.OneOf {
		all = append(all, group...)
	}
	return dedupe(all)
}

func hasAny(params map[string]any, names []string) bool {
	for _, name := range names {
		if _, ok := params[name]; ok {
			return true
		}
	}
	return false
}

func dedupe(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		parsed, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	default:
		return 0, false
	}
}
