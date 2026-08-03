# Configuration

Every way to turn Stagehand on and tune it. Back to the [README](../README.md).

| Method | Example |
|--------|---------|
| CLI flag | `godot --stagehand` |
| Env var | `STAGEHAND_ENABLED=1 godot ...` |
| Editor toggle | Stagehand button in toolbar |
| Release-export opt-in | `STAGEHAND_ENABLED=1 STAGEHAND_ALLOW_RELEASE=1 ./game` |
| Custom port | `STAGEHAND_PORT=9999` or `--stagehand-port=9999` (put it after `--`, e.g. `godot ... -- --stagehand-port=9999`; before `--` also works but logs a warning) |
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

Prefer the environment variable when installing into a project you didn't
write. Many host projects (editors, tools, anything with its own `--help`)
parse their own command-line arguments and will reject `--stagehand` with
`Unknown option: --stagehand` and quit, even though Stagehand itself started
fine. `STAGEHAND_ENABLED=1` bypasses argument parsing entirely, so it works
regardless of what the host project's CLI parser recognizes.

## Timeouts and concurrency

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

**Launch your own instance. That is the paved road.** `godot_launch(project_path=...)`
defaults to `port=0`, which auto-assigns a free port, so the game it starts is
private to the agent that started it. Reserve `godot_connect` for a game you
know is yours, and pass its explicit `port`.

Hosts that fan work out across agents can set `STAGEHAND_MULTI=1` on the MCP
server process. In that mode `godot_connect` refuses to fall back to the shared
default and requires an explicit `port`, so an accidental default connection
fails loudly instead of silently joining someone else's game. Single-instance
setups need no new arguments; leave `STAGEHAND_MULTI` unset.

See also [instance isolation](architecture/instance-isolation.md) for how two
instances of the same project avoid sharing `user://` and the import cache, and
the [security boundary](security.md) before changing the bind address or opting
into unsafe methods.
