package mcpserver

const hostSelectionDescription = "WebSocket host; use 127.0.0.1 for local/Linux Godot, localhost for WSL mirrored Windows Godot, or the WSL gateway IP for WSL NAT Windows Godot. Gateway access also requires STAGEHAND_BIND_ADDRESS=0.0.0.0, STAGEHAND_ALLOW_REMOTE=1, and the current auth_token"

const headlessScreenshotWarning = "headless=true is for structural tools such as godot_get_tree, godot_find_nodes, and godot_evaluate; godot_screenshot, baselines, and diffs require a visible rendered window. Relaunch with headless=false for visual workflows."

func connectionGuidance() string {
	return "Host guidance: use 127.0.0.1 when Godot and godot-stagehand run in the same OS/network namespace, including Linux Godot inside WSL. Use localhost for Windows Godot from WSL only when WSL mirrored networking is enabled. For Windows Godot from WSL NAT/default networking, use the WSL default gateway IP and start Godot with STAGEHAND_BIND_ADDRESS=0.0.0.0 plus STAGEHAND_ALLOW_REMOTE=1. Supply the current auth_token for every connection."
}
