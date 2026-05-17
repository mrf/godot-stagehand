# Godot Stagehand

External automation and testing for running Godot games — like Playwright, but for game engines.

An MCP server (Go) + Godot addon (GDScript) that lets AI agents, test runners, and CI pipelines connect to a running Godot game and interact with it programmatically.

## How It Works

```
┌─────────────┐       ┌──────────────────┐       ┌──────────────┐
│  MCP Client │◄─────►│  Go Server       │◄─────►│  Your Game   │
│  (Claude,   │ stdio │  (godot-stagehand)│  WS   │  (Godot +    │
│   CI, etc.) │       │                  │ :26700 │   addon)     │
└─────────────┘       └──────────────────┘       └──────────────┘
```

**The addon** lives inside your Godot game. It opens a WebSocket port and waits for commands. When it receives one (like "click this button" or "get the scene tree"), it executes it inside the running game and sends back the result. Think of it as a tiny remote control receiver baked into your game.

**The Go server** is the translator in the middle. It speaks MCP (the protocol AI tools like Claude use) on one side, and the Godot wire protocol on the other. Your MCP client never talks to Godot directly — the server handles connection management, selector parsing, and error handling so the addon can stay simple.

**Why not talk directly to Godot?** MCP clients communicate over stdio (stdin/stdout), not WebSockets. The Go server bridges that gap and adds features like retry logic, selector validation, and screenshot handling that would be awkward to implement in GDScript.

## Setup

### 1. Install the addon

```bash
./copy-addon.sh /path/to/your/godot/project
```

This copies the addon, enables the plugin, and registers the autoload automatically.

### 2. Run your game with Stagehand enabled

```bash
godot --path /path/to/your/project --stagehand
```

You should see `Stagehand: Server listening on port 26700` in the output.

### 3. Add to your MCP client

Add to `.claude/settings.json` (or your MCP client's config):

```json
{
  "mcpServers": {
    "godot-stagehand": {
      "command": "/absolute/path/to/godot-stagehand"
    }
  }
}
```

Build the server binary with `go build -o godot-stagehand .` or `go install .`

That's it. Call `godot_connect` from your MCP client to attach to the running game.

## Windows / WSL Setup

If you develop on Windows with WSL (Godot runs on Windows, MCP server runs in WSL):

**Option A: Mirrored networking (recommended)**

Create `C:\Users\<you>\.wslconfig`:
```ini
[wsl2]
networkingMode=mirrored
```
Restart WSL (`wsl --shutdown`). After this, `localhost` in WSL reaches Windows ports directly.

**Option B: Firewall rule**

Run in PowerShell as Administrator:
```powershell
New-NetFirewallRule -DisplayName "Stagehand Godot" -Direction Inbound -LocalPort 26700 -Protocol TCP -Action Allow
```

Then connect with the Windows host IP:
```json
{ "name": "godot_connect", "arguments": { "host": "172.x.x.x" } }
```
Find your WSL gateway IP with: `ip route show default | awk '{print $3}'`

**Launching Godot on Windows:**
```cmd
godot.exe --path "\\wsl.localhost\Ubuntu\home\you\project" --stagehand
```

## Available Tools

| Tool | Description |
|------|-------------|
| `godot_connect` | Connect to a running game |
| `godot_launch` | Launch Godot and connect |
| `godot_get_tree` | Snapshot the scene tree |
| `godot_find_nodes` | Find nodes by selector |
| `godot_get_property` / `godot_set_property` | Read/write node properties |
| `godot_call_method` | Call methods on nodes |
| `godot_evaluate` | Evaluate GDScript expressions |
| `godot_click` | Click nodes or positions |
| `godot_press_key` | Simulate keyboard input |
| `godot_press_action` | Trigger input actions |
| `godot_touch` | Simulate touch/drag |
| `godot_type_text` | Type text into controls |
| `godot_mouse_move` | Move mouse cursor |
| `godot_screenshot` | Capture viewport |
| `godot_screenshot_save_baseline` / `godot_screenshot_diff` | Visual regression testing |
| `godot_wait_for_node` | Wait for node state |
| `godot_wait_for_signal` | Wait for signal emission |
| `godot_wait_for_property` | Wait for property condition |
| `godot_change_scene` | Change scenes |
| `godot_get_game_state` | Runtime info (scene, FPS, window) |
| `godot_get_performance` / `godot_assert_performance` | Performance monitoring |
| `godot_record_start` / `godot_record_stop` / `godot_replay` | Input recording/replay |

## Selectors

| Syntax | Example | Finds |
|--------|---------|-------|
| Path | `/root/UI/StartButton` | Node at exact path |
| Name | `name:*Button*` | Glob match on node name |
| Class | `class:Button` | All nodes of class |
| Group | `group:interactive` | All nodes in group |
| Text | `text:Start` | Nodes containing text |
| Meta | `meta:id=player` | Nodes with metadata |
| Chain | `class:Panel >> name:*Btn*` | Scoped search (find within) |

## Configuration

| Method | Example |
|--------|---------|
| CLI flag | `godot --stagehand` |
| Env var | `STAGEHAND_ENABLED=1 godot ...` |
| Editor toggle | Stagehand button in toolbar |
| Custom port | `STAGEHAND_PORT=9999` or `--stagehand-port=9999` |

## Building

```bash
go build -o godot-stagehand .          # build binary
go test ./...                           # run tests
./build-release.sh v0.2.0              # cross-platform release
```

Requires Go 1.25+ and Godot 4.3+.

## Troubleshooting

**"Connection refused"** — Game isn't running with `--stagehand`, or wrong host/port.

**"Connection reset"** — Godot started but `_process` isn't ticking (common in headless with heavy scenes). Use a visible window or a lighter scene.

**Port conflict** — Another instance on 26700. Set `STAGEHAND_PORT=26701`.

**Addon not in plugin list** — Run `./copy-addon.sh` again; it auto-enables the plugin and autoload.

## License

MIT — see [LICENSE](LICENSE).
