package gwp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// FormatError renders an addon-side handler failure as one human-readable
// line. Addons predating the canonical error model (see docs/error-model.md)
// report failures as a `{"error", "error_code", "details"}` triple inside an
// otherwise successful JSON-RPC result; this renders that legacy shape through
// the same formatter as a current addon's JSON-RPC error, so an upgrade does
// not change what a caller reads. Such a result names no method.
func FormatError(message string, code string, details map[string]any) string {
	return RemoteFailure{Code: code, Message: message, Details: details}.Describe()
}

// LegacyFailure decodes a pre-canonical in-result failure — the
// `{"error", "error_code", "details"}` triple an older addon embeds in an
// otherwise successful JSON-RPC result — reporting false when raw carries no
// such failure. Every frontend needs this identically, so that an upgraded Go
// binary talking to an addon still vendored in a host project does not hand a
// failure to the caller as a success.
//
// A non-string `error` value, or one this build cannot decode, still counts as
// a failure: the alternative is treating an unreadable failure as success.
func LegacyFailure(raw json.RawMessage) (RemoteFailure, bool) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return RemoteFailure{}, false // not an object; nothing to inspect
	}
	rawErr, ok := probe["error"]
	if !ok {
		return RemoteFailure{}, false
	}

	var payload struct {
		Error     string         `json:"error"`
		ErrorCode string         `json:"error_code"`
		Details   map[string]any `json:"details"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil && payload.Error != "" {
		return RemoteFailure{
			Code:    payload.ErrorCode,
			Message: payload.Error,
			Details: payload.Details,
		}, true
	}

	var message string
	if err := json.Unmarshal(rawErr, &message); err == nil && message != "" {
		return RemoteFailure{Message: message}, true
	}
	return RemoteFailure{Message: "godot handler error (unparseable)"}, true
}

// RemoteFailure is one addon-side handler failure, decoded from the canonical
// JSON-RPC error response the addon now sends. Every field except Message is
// optional: an older addon, or a protocol-level fault such as a parse error,
// supplies only the message.
type RemoteFailure struct {
	// Method is the GWP method that failed, e.g. "get_property".
	Method string
	// Selector is the target the failed call named, when it named one.
	Selector string
	// Code is the stable machine kind, e.g. "node_not_found".
	Code string
	// Message is the human-readable summary.
	Message string
	// Details is structured context. The "next_action" key, when present, is
	// promoted into the rendered line as the remediation hint.
	Details map[string]any
}

// nextActionKey is the details entry every handler is expected to supply with
// an actionable remediation step. It is rendered separately from the rest of
// the details so an agent reading the tool result sees what to do next without
// having to parse a JSON blob.
const nextActionKey = "next_action"

// Describe renders a failure as one human-readable line that names what failed,
// what it was pointed at, and — when the addon supplied one — what to do about
// it. The rendering is deterministic so tests and trace reports can assert on
// it: fixed field order, and remaining details sorted by key.
//
//	get_property failed: Property not found: hp (code=property_not_found, selector=name:Player) — Call get_tree ... [node_class=Node2D]
func (f RemoteFailure) Describe() string {
	var b strings.Builder
	if f.Method != "" {
		b.WriteString(f.Method)
		b.WriteString(" failed: ")
	}
	if f.Message != "" {
		b.WriteString(f.Message)
	} else {
		b.WriteString("Godot reported a handler failure with no message")
	}

	var attrs []string
	if f.Code != "" {
		attrs = append(attrs, "code="+f.Code)
	}
	if f.Selector != "" {
		attrs = append(attrs, fmt.Sprintf("selector=%q", f.Selector))
	}
	if len(attrs) > 0 {
		b.WriteString(" (")
		b.WriteString(strings.Join(attrs, ", "))
		b.WriteString(")")
	}

	if hint, ok := f.Details[nextActionKey].(string); ok && hint != "" {
		b.WriteString(" — ")
		b.WriteString(hint)
	}

	if rest := formatDetails(f.Details); rest != "" {
		b.WriteString(" [")
		b.WriteString(rest)
		b.WriteString("]")
	}
	return b.String()
}

// formatDetails renders every details entry except next_action (already
// promoted into the message) as a sorted, comma-separated key=value list.
func formatDetails(details map[string]any) string {
	keys := make([]string, 0, len(details))
	for key := range details {
		if key == nextActionKey {
			continue
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, formatDetailValue(details[key])))
	}
	return strings.Join(parts, ", ")
}

// formatDetailValue keeps scalars unquoted and compact — JSON numbers arrive as
// float64, and rendering 8 as "8" rather than "8e+00" matters for readability —
// and falls back to JSON for anything composite.
func formatDetailValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case nil:
		return "null"
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(encoded)
	}
}
