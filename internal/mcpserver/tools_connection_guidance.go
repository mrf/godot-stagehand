package mcpserver

const hostSelectionDescription = "WebSocket host; use 127.0.0.1 for local/Linux Godot, localhost for WSL mirrored Windows Godot, or the WSL gateway IP for WSL NAT Windows Godot"

const headlessScreenshotWarning = "headless=true is for structural tools such as godot_get_tree, godot_find_nodes, and godot_evaluate; godot_screenshot, baselines, and diffs require a visible rendered window. Relaunch with headless=false for visual workflows."

func connectionGuidance() string {
	return "Host guidance: use 127.0.0.1 when Godot and godot-stagehand run in the same OS/network namespace, including Linux Godot inside WSL. Use localhost for Windows Godot from WSL only when WSL mirrored networking is enabled. Use the WSL default gateway IP for Windows Godot from WSL NAT/default networking."
}
