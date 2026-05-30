# Godot Stagehand

External automation and testing for running Godot games — like Playwright, but for game engines.

## Why

Game testing is manual. You click through menus, eyeball the results, and hope you caught the regressions. Automated UI tests exist for the web, but Godot has nothing equivalent — no way for an external process to connect to a running game and drive it programmatically.

Stagehand fixes that. It gives AI agents, test runners, and CI pipelines a real connection to your running game. Click buttons, read properties, wait for signals, take screenshots, assert performance — all from outside the engine.

## What you can do

- **AI-assisted playtesting** — Let Claude (or any MCP client) explore your game, find bugs, and verify fixes without manual clicking.
- **Visual regression testing** — Save baseline screenshots, diff them later. Catch UI regressions before your players do.
- **Integration tests** — Write tests that drive your actual game: navigate menus, trigger gameplay, assert on real game state.
- **CI pipelines** — Run headless Godot in CI, connect Stagehand, and gate merges on automated gameplay checks.
- **Performance monitoring** — Poll engine performance counters and fail builds when frame times regress.
- **Input recording/replay** — Record a play session, replay it deterministically for regression testing.

## How it works

```
┌─────────────┐       ┌──────────────────┐       ┌──────────────┐
│  MCP Client │◄─────►│  Go Server       │◄─────►│  Your Game   │
│  (Claude,   │ stdio │  (godot-stagehand)│  WS   │  (Godot +    │
│   CI, etc.) │       │                  │ :26700 │   addon)     │
└─────────────┘       └──────────────────┘       └──────────────┘
```

**The addon** lives inside your Godot game. It opens a WebSocket port and waits for commands. When it receives one (like "click this button" or "get the scene tree"), it executes it inside the running game and sends back the result.

**The Go server** sits in the middle. It speaks MCP (the protocol AI tools use) on one side and the Godot wire protocol on the other. It handles connection management, selector parsing, screenshot encoding, and error translation so the addon stays simple.

## Available tools

| Tool | Description |
|------|-------------|
| `godot_connect` | Connect to a running game |
| `godot_launch` | Launch Godot and connect |
| `godot_status` | Connection status |
| `godot_get_tree` | Snapshot the scene tree |
| `godot_find_nodes` | Find nodes by selector |
| `godot_get_property` / `godot_set_property` | Read/write node properties |
| `godot_call_method` | Call methods on nodes |
| `godot_evaluate` | Evaluate GDScript expressions |
| `godot_click` | Click nodes or coordinates |
| `godot_press_key` | Simulate keyboard input |
| `godot_press_action` | Trigger input actions |
| `godot_touch` | Simulate touch/drag |
| `godot_type_text` | Type text into controls |
| `godot_mouse_move` | Move mouse cursor |
| `godot_screenshot` | Capture viewport |
| `godot_screenshot_save_baseline` / `godot_screenshot_diff` | Visual regression testing |
| `godot_wait_for_node` | Wait for node to exist |
| `godot_wait_for_signal` | Wait for signal emission |
| `godot_wait_for_property` | Wait for property condition |
| `godot_change_scene` | Change scenes |
| `godot_get_game_state` | Runtime info (scene, FPS, window) |
| `godot_get_performance` / `godot_assert_performance` | Performance monitoring |
| `godot_record_start` / `godot_record_stop` / `godot_replay` | Input recording/replay |

## Selectors

Target nodes using familiar patterns:

| Syntax | Example | Finds |
|--------|---------|-------|
| Path | `/root/UI/StartButton` | Node at exact path |
| Name | `name:*Button*` | Glob match on node name |
| Class | `class:Button` | All nodes of class |
| Group | `group:interactive` | All nodes in group |
| Text | `text:Start` | Nodes containing text |
| Meta | `meta:id=player` | Nodes with metadata |
| Chain | `class:Panel >> name:*Btn*` | Scoped search (find within) |

## Setup

### 1. Install the addon into your Godot project

```bash
./copy-addon.sh /path/to/your/godot/project
```

This copies the addon, enables the plugin, and registers the autoload.

### 2. Build the server

```bash
go build -o godot-stagehand .
```

Requires Go 1.25+ and Godot 4.3+.

### 3. Run your game with Stagehand enabled

```bash
godot --path /path/to/your/project --stagehand
```

You should see `Stagehand: Server listening on port 26700` in the output.

### 4. Add to your MCP client

Add to your MCP client config (e.g. `.claude/settings.json`):

```json
{
  "mcpServers": {
    "godot-stagehand": {
      "command": "/absolute/path/to/godot-stagehand"
    }
  }
}
```

Call `godot_connect` to attach to the running game. Local Linux/macOS and Linux Godot inside WSL use `127.0.0.1` by default. For Windows Godot controlled from WSL, use `localhost` with WSL mirrored networking or the WSL default gateway IP with NAT/default networking.

> **Windows / WSL?** See the [Windows setup guide](docs/windows-setup.md).

## Configuration

| Method | Example |
|--------|---------|
| CLI flag | `godot --stagehand` |
| Env var | `STAGEHAND_ENABLED=1 godot ...` |
| Editor toggle | Stagehand button in toolbar |
| Custom port | `STAGEHAND_PORT=9999` or `--stagehand-port=9999` |

## Troubleshooting

**"Connection refused"** — Game isn't running with `--stagehand`, or wrong host/port.

**"Connection reset"** — Godot started but `_process` isn't ticking (common in headless with heavy scenes). Use a visible window or a lighter scene.

**Screenshots are empty, black, or grey** — Visual workflows need a visible rendered window. Use `godot_launch(headless=false, expect_screenshots=true)`; headless launches are for structural tools.

**Port conflict** — Another instance on 26700. Set `STAGEHAND_PORT=26701`.

**Addon not in plugin list** — Run `./copy-addon.sh` again; it auto-enables the plugin and autoload.

## License

MIT — see [LICENSE](LICENSE).
