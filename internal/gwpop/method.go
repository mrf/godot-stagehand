package gwpop

import (
	"fmt"
	"strings"
)

// blockedMethods lists Godot methods that must not be called remotely.
// The GDScript side enforces the same list; this is defense-in-depth, and it
// lives here so every frontend (MCP tools, CLI, scenario runner) enforces the
// identical set rather than each keeping its own copy.
var blockedMethods = map[string]bool{
	"free":                   true,
	"queue_free":             true,
	"set_script":             true,
	"add_child":              true,
	"remove_child":           true,
	"queue_redraw":           true,
	"notification":           true,
	"propagate_notification": true,
	"set_process":            true,
	"set_physics_process":    true,
}

// ValidateMethodName returns an error when method may not be invoked remotely.
func ValidateMethodName(method string) error {
	if strings.HasPrefix(method, "_") {
		return fmt.Errorf("Blocked: private/lifecycle methods (starting with '_') cannot be called")
	}
	if blockedMethods[method] {
		return fmt.Errorf("Blocked: '%s' is a destructive method and cannot be called remotely", method)
	}
	return nil
}
