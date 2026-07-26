package mcpserver

import (
	"fmt"
	"math"

	"github.com/mark3labs/mcp-go/mcp"
)

// maxUnboundedMs is the ceiling used for millisecond duration params whose
// schema declares only mcp.Min, no mcp.Max (hold_ms, delay_ms, duration_ms).
// It is not a schema bound — it exists so boundedInt's float64->int narrowing
// never overflows: float64 stops being able to represent every integer
// exactly above 2^53, so the range check itself becomes unreliable well
// before int64 overflow, and a hold/delay duration has no legitimate reason
// to approach it. ~24.8 days in milliseconds.
const maxUnboundedMs = math.MaxInt32

// mcp-go validates a tool call's arguments against the JSON-schema *shape*
// (types, required fields) but not against mcp.Min/mcp.Max — those are
// advisory metadata for the client, not a server-side check. boundedNumber
// and boundedInt are the single point where a declared bound is actually
// enforced, so the declared bound (in a tool's var ...Tool = mcp.NewTool(...)
// block) and the enforced bound can never drift apart. Every handler that
// reads a bounded numeric argument must go through one of these instead of
// req.GetInt/req.GetFloat directly.
//
// Bound-checking runs on the raw float64 value before any narrowing to int:
// narrowing an out-of-range float64 (e.g. 1e19) to int first is what let a
// bogus timeout_ms saturate to math.MinInt64 and silently produce a
// near-immediate deadline instead of being rejected (godot-stagehand-f9o3).

// argFloat extracts a raw numeric argument as float64. ok is false when the
// key is present but not a JSON number (mcp-go decodes all JSON numbers as
// float64, so int/string never appear here in practice, but a caller-forged
// arguments map is not guaranteed to).
func argFloat(req mcp.CallToolRequest, key string) (value float64, present, ok bool) {
	raw, has := req.GetArguments()[key]
	if !has {
		return 0, false, true
	}
	switch v := raw.(type) {
	case float64:
		return v, true, true
	case int:
		return float64(v), true, true
	default:
		return 0, true, false
	}
}

// boundedNumber reads a float64 argument named key, applying def when absent
// and rejecting a present value outside [min, max] (inclusive) or non-finite.
func boundedNumber(req mcp.CallToolRequest, key string, def, min, max float64) (float64, *mcp.CallToolResult) {
	value, present, ok := argFloat(req, key)
	if !present {
		return def, nil
	}
	if !ok {
		return 0, mcp.NewToolResultError(fmt.Sprintf("parameter %q must be a number", key))
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, mcp.NewToolResultError(fmt.Sprintf("parameter %q must be a finite number", key))
	}
	if value < min || value > max {
		return 0, mcp.NewToolResultError(fmt.Sprintf(
			"parameter %q must be between %s and %s (got %s)",
			key, formatBound(min), formatBound(max), formatBound(value),
		))
	}
	return value, nil
}

// boundedInt is boundedNumber narrowed to int, for wire params that are
// logically integers (timeout_ms, limit, ...). Range-checking happens on the
// float64 value before narrowing, so an out-of-range value is rejected
// instead of silently saturating through a direct int(float64) conversion.
func boundedInt(req mcp.CallToolRequest, key string, def, min, max int) (int, *mcp.CallToolResult) {
	value, errResult := boundedNumber(req, key, float64(def), float64(min), float64(max))
	if errResult != nil {
		return 0, errResult
	}
	return int(value), nil
}

func formatBound(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%g", f)
}
