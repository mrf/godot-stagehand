package gwpop

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/mrf/godot-stagehand/internal/godotconn"
	"github.com/mrf/godot-stagehand/internal/gwp"
	"github.com/mrf/godot-stagehand/internal/visual"
)

// Operators is the comparison vocabulary shared by scenario assertions and the
// addon's wait_for_property handler.
var Operators = []string{"equals", "not_equals", "contains", "exists", "greater_than", "less_than"}

// Compare evaluates `actual <operator> expected`. It is deliberately strict:
// an ordering comparison on non-numbers is a scenario authoring bug, not a
// silent false, because a silently-false assertion reads as a real regression.
func Compare(operator string, actual, expected any) (bool, error) {
	switch operator {
	case "exists":
		return actual != nil, nil
	case "equals":
		return valuesEqual(actual, expected), nil
	case "not_equals":
		return !valuesEqual(actual, expected), nil
	case "contains":
		return containsValue(operator, actual, expected)
	case "greater_than", "less_than":
		left, ok := asFloat(actual)
		if !ok {
			return false, fmt.Errorf("operator %q needs a numeric actual value, got %s", operator, describe(actual))
		}
		right, ok := asFloat(expected)
		if !ok {
			return false, fmt.Errorf("operator %q needs a numeric expected value, got %s", operator, describe(expected))
		}
		if operator == "greater_than" {
			return left > right, nil
		}
		return left < right, nil
	default:
		return false, fmt.Errorf("unknown operator %q (known: %s)", operator, strings.Join(Operators, ", "))
	}
}

// PropertyValue extracts the `value` field from a get_property result.
func PropertyValue(raw json.RawMessage) (any, error) {
	var payload struct {
		Value any `json:"value"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse get_property response: %w", err)
	}
	return payload.Value, nil
}

// NodeCount extracts the total match count from a query_nodes result. The
// count is the number of nodes the selector matched, which can exceed the
// number returned when a limit was applied.
func NodeCount(raw json.RawMessage) (int, error) {
	var payload struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0, fmt.Errorf("failed to parse query_nodes response: %w", err)
	}
	return payload.Count, nil
}

// PerformanceOutcome is the shape of an assert_performance result.
type PerformanceOutcome struct {
	Passed    bool    `json:"passed"`
	Monitor   string  `json:"monitor"`
	Value     float64 `json:"value"`
	Threshold float64 `json:"threshold"`
	Op        string  `json:"op"`
	Message   string  `json:"message,omitempty"`
}

// DecodePerformance parses an assert_performance result.
func DecodePerformance(raw json.RawMessage) (PerformanceOutcome, error) {
	var outcome PerformanceOutcome
	if err := json.Unmarshal(raw, &outcome); err != nil {
		return outcome, fmt.Errorf("failed to parse assert_performance response: %w", err)
	}
	return outcome, nil
}

// Connect dials an already-running Godot session, authenticates, and
// negotiates the wire protocol. It is the one place a non-MCP frontend obtains
// a usable connection, so the CLI and the scenario runner cannot diverge on
// which handshake failures are fatal.
func Connect(ctx context.Context, host string, port int, token string) (*godotconn.Connection, *gwp.Info, error) {
	conn, err := godotconn.Dial(ctx, host, port)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to Godot at %s:%d: %w", host, port, err)
	}
	if err := conn.Authenticate(ctx, token); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("authenticate with Godot at %s:%d: %w", host, port, err)
	}
	resp, err := conn.Call(ctx, "ping", nil)
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("ping Godot at %s:%d: %w", host, port, err)
	}
	info, err := gwp.Negotiate(resp.Result)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return conn, info, nil
}

// Capture performs a screenshot RPC and decodes the frame.
func Capture(ctx context.Context, c Caller, nodeSelector string) (visual.Shot, error) {
	params := map[string]any{}
	if nodeSelector != "" {
		params["selector"] = nodeSelector
	}
	raw, err := Execute(ctx, c, Op{Action: "screenshot", Params: params})
	if err != nil {
		return visual.Shot{}, err
	}
	shot, err := visual.Decode(raw)
	if err != nil {
		return visual.Shot{}, newError(KindRemote, "screenshot", "%v", err)
	}
	return shot, nil
}

func valuesEqual(actual, expected any) bool {
	if left, ok := asFloat(actual); ok {
		if right, ok := asFloat(expected); ok {
			return left == right
		}
	}
	return reflect.DeepEqual(actual, expected)
}

func containsValue(operator string, actual, expected any) (bool, error) {
	switch container := actual.(type) {
	case string:
		needle, ok := expected.(string)
		if !ok {
			return false, fmt.Errorf("operator %q on a string needs a string expected value, got %s", operator, describe(expected))
		}
		return strings.Contains(container, needle), nil
	case []any:
		for _, item := range container {
			if valuesEqual(item, expected) {
				return true, nil
			}
		}
		return false, nil
	case map[string]any:
		key, ok := expected.(string)
		if !ok {
			return false, fmt.Errorf("operator %q on an object needs a string key, got %s", operator, describe(expected))
		}
		_, present := container[key]
		return present, nil
	default:
		return false, fmt.Errorf("operator %q needs a string, array or object actual value, got %s", operator, describe(actual))
	}
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		parsed, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func describe(v any) string {
	if v == nil {
		return "null"
	}
	return fmt.Sprintf("%T", v)
}
