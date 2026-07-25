package mcpserver

import (
	"fmt"
	"os"
	"strconv"

	"github.com/mrf/godot-stagehand/internal/gwp"
	"github.com/mrf/godot-stagehand/internal/launch"
)

// defaultSharedPort is the port the addon listens on when STAGEHAND_PORT is unset.
const defaultSharedPort = launch.DefaultPort

// multiInstanceEnvVar opts a host into strict multi-instance mode, where
// godot_connect refuses to fall back to the shared default port. It is off by
// default so the single-instance flow keeps working with no extra arguments.
const multiInstanceEnvVar = "STAGEHAND_MULTI"

const hostSelectionDescription = "WebSocket host; use 127.0.0.1 for local/Linux Godot, localhost for WSL mirrored Windows Godot, or the WSL gateway IP for WSL NAT Windows Godot. Gateway access also requires STAGEHAND_BIND_ADDRESS=0.0.0.0, STAGEHAND_ALLOW_REMOTE=1, and the current auth_token"

const headlessScreenshotWarning = "headless=true is for structural tools such as godot_get_tree, godot_find_nodes, and godot_evaluate; godot_screenshot, baselines, and diffs require a visible rendered window. Relaunch with headless=false for visual workflows."

// multiInstanceModeEnabled reports whether the host asked for strict
// multi-instance behaviour via STAGEHAND_MULTI. Unparseable values are treated as
// disabled so a typo never breaks the ordinary single-instance flow.
func multiInstanceModeEnabled() bool {
	raw := os.Getenv(multiInstanceEnvVar)
	if raw == "" {
		return false
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return false
	}
	return enabled
}

// explicitPortGuidance explains why the shared default port was refused.
func explicitPortGuidance() string {
	return fmt.Sprintf(
		"%s is set, so godot_connect requires an explicit port: connecting on the shared default %d would attach to whatever Godot another agent already started. "+
			"Prefer godot_launch (port 0 auto-assigns a free port) to get your own game, or pass the port that your own instance printed at startup.",
		multiInstanceEnvVar, defaultSharedPort,
	)
}

// sharedPortWarning flags a connection that landed on the shared default port.
func sharedPortWarning(port int) string {
	if port != defaultSharedPort {
		return ""
	}
	return fmt.Sprintf(
		"WARNING: port %d is the shared default. Any other agent connecting with defaults drives this same game, so parallel work can cross-talk and tests can go nondeterministic. Use godot_launch to get a private instance on an auto-assigned port.",
		defaultSharedPort,
	)
}

// formatConnectSuccess renders the godot_connect success payload, appending the
// negotiated protocol summary and the shared-port warning when one applies.
func formatConnectSuccess(host string, port int, instanceID string, payload string, handshake *gwp.Info) string {
	text := fmt.Sprintf("Connected to Godot at %s:%d (instance_id=%q)\n%s", host, port, instanceID, payload)
	if handshake != nil {
		text += "\n\n" + handshake.Summary()
	}
	if warning := sharedPortWarning(port); warning != "" {
		text += "\n\n" + warning
	}
	return text
}

func connectionGuidance() string {
	return "Host guidance: use 127.0.0.1 when Godot and godot-stagehand run in the same OS/network namespace, including Linux Godot inside WSL. Use localhost for Windows Godot from WSL only when WSL mirrored networking is enabled. For Windows Godot from WSL NAT/default networking, use the WSL default gateway IP and start Godot with STAGEHAND_BIND_ADDRESS=0.0.0.0 plus STAGEHAND_ALLOW_REMOTE=1. Supply the current auth_token for every connection."
}
