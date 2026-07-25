# Godot Stagehand

[![Go Report Card](https://goreportcard.com/badge/github.com/mrf/godot-stagehand)](https://goreportcard.com/report/github.com/mrf/godot-stagehand)

External automation and testing for running Godot games — like Playwright, but for game engines.

**New to Stagehand?** → [Quickstart guide](docs/quickstart.md) — zero to connected in 5 minutes, no Go or JSON experience required.

## Why

Game testing is manual. You click through menus, eyeball the results, and hope you caught the regressions. Automated UI tests exist for the web, but Godot has nothing equivalent — no way for an external process to connect to a running game and drive it programmatically.

Stagehand fixes that. It gives AI agents, test runners, and CI pipelines a real connection to your running game. Click buttons, read properties, wait for signals, take screenshots, assert performance — all from outside the engine.

## What you can do

- **AI-assisted playtesting** — Let Claude (or any MCP client) explore your game, find bugs, and verify fixes without manual clicking.
- **Visual regression testing** — Save baseline screenshots, diff them later. Catch UI regressions before your players do. See the [visual smoke contract](docs/visual-smoke-contract.md) for how to set up a visual gate in your game repo.
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
| `godot_connect` | Authenticate and connect to a running game |
| `godot_launch` | Launch Godot with a fresh session secret and connect |
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
| `godot_screenshot_save_baseline` / `godot_screenshot_diff` | Visual regression testing ([guide](docs/visual-regression.md)) |
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

### 1. Get the server binary

Download the prebuilt binary for your platform from the [latest release](https://github.com/mrf/godot-stagehand/releases/latest):

| Platform | File |
|----------|------|
| Linux x86-64 | `godot-stagehand-linux-amd64` |
| macOS Apple Silicon | `godot-stagehand-darwin-arm64` |
| macOS Intel | `godot-stagehand-darwin-amd64` |
| Windows x86-64 | `godot-stagehand-windows-amd64.exe` |

macOS/Linux: mark the downloaded binary executable with `chmod +x godot-stagehand-*`.

**From source** (requires Go 1.25+ and Godot 4.3+):

```bash
go build -o godot-stagehand .
```

### 2. Install into your Godot project (one command)

```bash
godot-stagehand setup /path/to/your/godot/project
```

This copies the addon, enables the plugin, and registers the `StagehandServer`
autoload — idempotently, so it is safe to re-run. It then prints the MCP client
config snippet (with this binary's detected path) and the exact command to run
your game. Pass `--force` to overwrite an existing addon installation. On WSL it
also prints WSL-specific connection guidance.

> The old `./copy-addon.sh` script is deprecated; it now forwards to
> `godot-stagehand setup`.

### 3. Run your game with Stagehand enabled

```bash
godot --path /path/to/your/project --stagehand
```

You should see a one-session authentication token followed by
`Stagehand: Server listening on port 26700 (127.0.0.1)` in the output. Keep the
token private; use it as the `auth_token` argument to `godot_connect`.

### 4. Add to your MCP client

The `setup` command prints this snippet for you; add it to your MCP client config
(e.g. `.claude/settings.json`):

```json
{
  "mcpServers": {
    "godot-stagehand": {
      "command": "/absolute/path/to/godot-stagehand"
    }
  }
}
```

Call `godot_connect` with the startup `auth_token` to attach to the running
game. Local Linux/macOS and Linux Godot inside WSL use `127.0.0.1` by default.
For Windows Godot controlled from WSL, see the remote-bind opt-in in the
[Windows setup guide](docs/windows-setup.md).

> **Windows / WSL?** See the [Windows setup guide](docs/windows-setup.md).

## Configuration

| Method | Example |
|--------|---------|
| CLI flag | `godot --stagehand` |
| Env var | `STAGEHAND_ENABLED=1 godot ...` |
| Editor toggle | Stagehand button in toolbar |
| Release-export opt-in | `STAGEHAND_ENABLED=1 STAGEHAND_ALLOW_RELEASE=1 ./game` |
| Custom port | `STAGEHAND_PORT=9999` or `--stagehand-port=9999` |
| Fixed authentication token | `STAGEHAND_AUTH_TOKEN=<secret>` (otherwise a fresh token is generated and printed) |
| Bind address | `STAGEHAND_BIND_ADDRESS=127.0.0.1` (loopback is the default) |
| Remote access | `STAGEHAND_BIND_ADDRESS=0.0.0.0 STAGEHAND_ALLOW_REMOTE=1` |
| Unsafe methods | `STAGEHAND_ALLOW_UNSAFE=1` or `godot_launch(allow_unsafe=true)` |
| Ordinary RPC timeout | `STAGEHAND_CALL_TIMEOUT_MS=30000` (default; 1–86400000 milliseconds) |
| Strict multi-instance mode | `STAGEHAND_MULTI=1` on the MCP server process (makes `godot_connect`'s `port` mandatory) |

The editor toggle is stored in editor-only project metadata and injects
`--stagehand` only when the editor launches the game. It is never written as a
runtime project setting. Release exports ignore the ordinary CLI flag and
`STAGEHAND_ENABLED` unless `STAGEHAND_ALLOW_RELEASE=1` is also set deliberately.

Ordinary Godot tool calls time out after 30 seconds so a frozen game cannot
occupy an MCP worker indefinitely. Set `STAGEHAND_CALL_TIMEOUT_MS` on the MCP
server process to deliberately change that default. The `godot_wait_*` tools
instead honor their explicit `timeout_ms` values plus a short transport buffer.
At most four remote Godot operations run concurrently, preserving one MCP
worker for local status and disconnect requests if the game freezes.
The WebSocket transport sends a ping every 10 seconds and requires a pong or
other inbound message within 30 seconds; a silent peer triggers reconnection.

## Running several agents at once

The addon accepts many WebSocket clients into **one** SceneTree, and port 26700 is
its default. Two agents that both call `godot_connect` with defaults therefore
drive the same game: their input, property writes, and scene changes interleave,
and tests stop being reproducible. `instance_id` isolates connections only within
a single MCP server process, not across processes.

**Launch your own instance — that is the paved road.** `godot_launch(project_path=...)`
defaults to `port=0`, which auto-assigns a free port, so the game it starts is
private to the agent that started it. Reserve `godot_connect` for a game you
know is yours, and pass its explicit `port`.

Hosts that fan work out across agents can set `STAGEHAND_MULTI=1` on the MCP
server process. In that mode `godot_connect` refuses to fall back to the shared
default and requires an explicit `port`, so an accidental default connection
fails loudly instead of silently joining someone else's game. Single-instance
setups need no new arguments — leave `STAGEHAND_MULTI` unset.

## Security boundary

Stagehand is a development automation control plane, not a public game
endpoint. It binds to `127.0.0.1` by default and rejects every command on each
WebSocket peer until that peer supplies the current session token. `godot_launch`
creates and authenticates with a fresh secret automatically; manual/editor
starts generate one and print it in the local Godot output.

Remote binding requires both a non-loopback `STAGEHAND_BIND_ADDRESS` and
`STAGEHAND_ALLOW_REMOTE=1`, and emits a prominent warning. Use it only on a
trusted network with an appropriate host firewall, and never publish the token.
The WebSocket transport is not encrypted; this boundary is not a substitute for
TLS, network isolation, or a trustworthy local host.
Expression evaluation and arbitrary method calls are disabled unless the
session separately opts into unsafe capabilities. Authentication limits who can
reach automation; unsafe opt-in controls what an authenticated peer may execute.

## Godot version compatibility

**Minimum supported version: Godot 4.3.** Development happens against 4.6.x
locally; 4.3-4.7 are all tested and supported.

| Godot version | Status | Notes |
|----------------|--------|-------|
| 4.2 | **Not supported** | Addon fails to parse — see below |
| 4.3 | Supported (minimum) | |
| 4.4 | Supported | |
| 4.5 | Supported | |
| 4.6 | Supported (local dev baseline) | |
| 4.7 | Supported | |

Verified by running the full connect-and-drive protocol (parse → activate →
authenticated ping → `get_tree`/`find_nodes`/`click`/`screenshot`) against a
real headless Godot binary of each version — see `scripts/test-godot-compat.sh`
and the `gdscript-parse` job in `.github/workflows/ci.yml`, which runs this
matrix on every push/PR to `main`.

### Known incompatibilities

- **Godot 4.2 — GDScript `is not` operator not available.** The addon uses
  `is not` (e.g. `internal/godotconn`'s JSON-RPC decoding, `stagehand_server.gd`,
  `input_recorder.gd`) for readability. That operator was added in
  [godotengine/godot#87939](https://github.com/godotengine/godot/pull/87939),
  first released in Godot 4.3, so it fails to parse on 4.2 with
  `Parse Error: Expected type specifier after "is"`. There is no workaround
  short of rewriting those checks as `not (x is T)`; 4.2 is treated as
  unsupported rather than carrying that rewrite for one older release.

## Troubleshooting

**"Connection refused"** — Game isn't running with `--stagehand`, or wrong host/port.

**"Authentication required/failed"** — Pass the token printed by this Godot
session, or its configured `STAGEHAND_AUTH_TOKEN`, as
`godot_connect(auth_token=...)`. Generated tokens from prior runs do not work.

**"Connection reset"** — Godot started but `_process` isn't ticking (common in headless with heavy scenes). Use a visible window or a lighter scene.

**Screenshots are empty, black, or grey** — Visual workflows need a visible rendered window. Use `godot_launch(headless=false, expect_screenshots=true)`; headless launches are for structural tools.

**Port conflict** — Another instance on 26700. Set `STAGEHAND_PORT=26701`.

**Addon not in plugin list** — Run `godot-stagehand setup /path/to/project` again; it idempotently enables the plugin and autoload.

## Development

```bash
go vet ./...          # lint
go test ./...         # Go tests (no Godot needed)

# GDScript unit suite (GdUnit4, headless — needs Godot 4.6+)
GODOT_BIN=/path/to/godot ./scripts/run-gdscript-tests.sh
```

See the [GDScript testing guide](docs/gdscript-testing.md) for the suite layout
and the strict-mode rules test files must follow, and the
[addon sync contract](docs/addon-sync-contract.md) before editing any copy of
the addon.

## License

MIT — see [LICENSE](LICENSE).
