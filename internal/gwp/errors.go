package gwp

import (
	"encoding/json"
	"fmt"
)

// FormatError renders an addon-side handler failure as one human-readable
// line. The addon reports failures as a `{"error", "error_code", "details"}`
// triple inside an otherwise successful JSON-RPC result, so every frontend —
// the MCP server, the CLI, the scenario runner — needs the same rendering.
func FormatError(message string, code string, details map[string]any) string {
	if code != "" {
		message += fmt.Sprintf(" (code=%s)", code)
	}
	if len(details) > 0 {
		encoded, err := json.Marshal(details)
		if err == nil {
			message += fmt.Sprintf(" details=%s", encoded)
		}
	}
	return message
}
