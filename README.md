# Godot Stagehand

[![CI](https://github.com/mrf/godot-stagehand/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/mrf/godot-stagehand/actions/workflows/ci.yml)
[![GitHub issues](https://img.shields.io/github/issues/mrf/godot-stagehand)](https://github.com/mrf/godot-stagehand/issues)
[![GitHub pull requests](https://img.shields.io/github/issues-pr/mrf/godot-stagehand)](https://github.com/mrf/godot-stagehand/pulls)
[![License](https://img.shields.io/github/license/mrf/godot-stagehand)](LICENSE)

External automation for running Godot games, exposed as an MCP server for AI agents and as a CLI plus scenario runner for CI and terminal debugging. Think Playwright, but for game engines.

> ### Stagehand drives your *running game*, not the Godot editor.
>
> It has no editor integration, cannot create a scene, and never touches your
> project files. It attaches to a game that is **already running and
> playing** — clicking real buttons, reading real node state at runtime,
> taking real screenshots of real frames.
>
> What sets it apart from other runtime-capable tools: **automated visual
> regression** (`screenshot_diff` against saved baselines, not just raw
> capture) and a **standalone CI scenario runner** — JUnit output and exit
> codes, usable in any pipeline without an MCP client.
>
> Editor-automation tools and Stagehand are complements, not competitors —
> plenty of projects will want both.

**Status: beta, pre-1.0.** Prebuilt binaries are published for Linux,
macOS (Intel and Apple Silicon) and Windows — see [Setup](#setup).
Tool schemas and the wire protocol may still change between minor versions.

**New to Stagehand?** → [Quickstart guide](docs/quickstart.md) for a full walkthrough, no Go or JSON experience required.

**Want to see it work before reading further?** → [`examples/minimal-game`](examples/minimal-game#watch-an-agent-play-it-one-command)
is a one-command on-ramp: clone, run one command, watch an agent click a
button in a real running Godot scene and assert the result. No demo video
exists yet — this is the fastest way to see the loop in action until one does.

## Why

Game testing is manual. You click through menus, eyeball the results, and hope
you caught the regressions. Godot's own testing tools (GUT, GdUnit4) run
in-process, inside the editor or a headless engine instance, so they don't give
an *external* process a live connection to a running game. The Godot MCP servers
that exist are aimed at the other half of the problem: helping an agent *author*
a project from inside the editor, which leaves the running game just as
unobservable as before.

Stagehand fills the runtime half of that gap.

Stagehand gives an MCP client (Claude, another AI agent, or your own
MCP-calling script) a real connection to your running game. Click buttons,
read properties, wait for signals, take screenshots, assert performance, all
from outside the engine.

## For AI agents (MCP)

Point an MCP client — Claude Code, Claude Desktop, Cursor, anything that
speaks MCP — at the binary and it gets a live connection to your running
game: click buttons, read node state, wait for signals, take screenshots,
assert performance, record and replay input.

```json
{
  "mcpServers": {
    "godot-stagehand": {
      "command": "/absolute/path/to/godot-stagehand"
    }
  }
}
```

- **AI-assisted playtesting.** Let Claude explore your game, find bugs, and verify fixes without manual clicking.
- **Visual regression testing.** Save baseline screenshots, diff them later. Catch UI regressions before your players do. See the [visual smoke contract](docs/visual-smoke-contract.md) for how to set up a visual gate in your game repo. **Headless Godot cannot render real screenshots**, so this needs a visible window (a real display or something like Xvfb) even in CI.
- **Input recording/replay.** Record a play session's input events with millisecond timestamps, then replay them on the same wall-clock schedule, optionally sped up. This reproduces a rough repro case, not a frame-perfect deterministic run: actual game state during replay still depends on frame timing, which can vary between runs. The on-disk format is versioned; see the [recording format](docs/recording-format.md).
- **Performance monitoring.** `godot_assert_performance` can sample and assert monitors: an optional warm-up, a fixed sample count or duration, and a statistic (min, max, mean, median, p95) to threshold against, instead of one instantaneous read. This is still not proven statistical regression gating (no baseline tracking, outlier rejection, or variance-aware thresholds), so treat it as a steadier smoke check, not a certified regression gate.
- **Agent skill.** [`skills/stagehand.md`](skills/stagehand.md) teaches an agent the full tool workflow — launch, inspect, interact, test — so you don't have to re-explain it in every session. Point your agent at the file directly, or copy it into wherever your client loads custom skills from (e.g. a Claude Code project's `.claude/skills/` directory).

Full walkthrough: [Quickstart guide](docs/quickstart.md). Full tool list: [Available tools](#available-tools) below.

## For CI and test automation (CLI)

The same binary is also a standalone CLI and scenario runner — no MCP client
needed. Use it for terminal debugging or as a CI pipeline step.

```bash
# One-shot inspection and actions against a running game
export STAGEHAND_AUTH_TOKEN=<the token this Godot session printed>
godot-stagehand tree   --port 26788 --max-depth 3
godot-stagehand find   --port 26788 'class:Button' --properties text

# A whole scenario, no MCP client involved
godot-stagehand run scenarios/menu-smoke.json --out-dir ci-artifacts
```

- **CI checks.** `godot-stagehand run scenario.json` executes a declarative list of launch, action, wait and assertion steps against a real Godot build and exits nonzero on failure, so a pipeline step needs no MCP client and no wrapper script. It emits a JSON report, JUnit XML, screenshots/diffs, the engine's own log, and an RPC timing trace. Headless Godot covers structural checks (scene tree, properties, performance counters); visual steps need a rendered window.
- **Scripted exploration.** `tree`, `find`, `property get`, `input click`, `wait node` and friends are one-shot commands that print JSON — good for terminal debugging without writing a scenario file.
- **Stable exit codes.** `0` is success, `5` is a real regression (an assertion or visual diff failed), `6` is a timeout — see the [exit codes table](#command-line) for the full contract.

Full reference: [CLI and scenario runner guide](docs/cli.md), or the [Command line](#command-line) section below.

## How it works

```
┌─────────────┐  stdio  ┌───────────────────┐         ┌──────────────┐
│  MCP Client │◄───────►│                   │         │              │
│  (Claude,   │         │   godot-stagehand │◄───────►│  Your Game   │
│   Cursor…)  │         │      (one Go      │   WS    │  (Godot +    │
├─────────────┤  argv   │      binary)      │ :26700  │   addon)     │
│  CLI / CI / │◄───────►│                   │         │              │
│  scenarios  │         └───────────────────┘         └──────────────┘
└─────────────┘
```

**The addon** lives inside your Godot game. It opens a WebSocket port and waits for commands. When it receives one (like "click this button" or "get the scene tree"), it executes it inside the running game and sends back the result.

**The Go binary** sits in the middle and speaks the Godot wire protocol on one side. On the other it offers two frontends over the same core: the MCP stdio protocol for AI agents, and a CLI with a scenario runner for pipelines and humans. Neither is built on the other. It handles connection management, selector parsing, screenshot encoding, and error translation so the addon stays simple.

Running the binary with **no arguments** serves MCP over stdio. That is what MCP client configurations invoke, and it has not changed.

## Available tools

| Tool | Description |
|------|-------------|
| `godot_connect` | Authenticate and connect to a running game |
| `godot_launch` | Launch Godot with a fresh session secret and connect |
| `godot_status` | Connection status |
| `godot_list_instances` | List all active Godot connections managed by this server |
| `godot_disconnect` | Disconnect and remove a named instance |
| `godot_get_tree` | Snapshot the scene tree |
| `godot_find_nodes` | Find nodes by selector |
| `godot_get_accessibility_tree` | Semantic UI view: roles, accessible names, states (Godot 4.5+) |
| `godot_get_property` / `godot_set_property` | Read/write node properties |
| `godot_call_method` | Call methods on nodes |
| `godot_evaluate` | Evaluate GDScript expressions |
| `godot_click` | Click nodes or coordinates |
| `godot_press_key` | Simulate keyboard input |
| `godot_press_action` | Trigger input actions |
| `godot_focus_window` | Focus a Window (e.g. a modal dialog) so key input reaches it |
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

## Command line

The same binary is an executable test runner. Full reference:
[docs/cli.md](docs/cli.md).

```bash
# One-shot inspection and actions against a running game
export STAGEHAND_AUTH_TOKEN=<the token this Godot session printed>
godot-stagehand tree         --port 26788 --max-depth 3
godot-stagehand find         --port 26788 'class:Button' --properties text
godot-stagehand property get --port 26788 'name:titleLabel' text
godot-stagehand input click  --port 26788 --selector 'text:Start'
godot-stagehand wait node    --port 26788 'name:Hud' --state visible
godot-stagehand performance  --port 26788 --assert TIME_FPS --threshold 55 --op gte

# A whole scenario, no MCP client involved
godot-stagehand run scenarios/menu-smoke.json --out-dir ci-artifacts
```

`run` launches (or connects to) Godot, executes ordered actions, waits and
assertions, and exits nonzero on failure. `--out-dir` collects `report.json`,
`junit.xml`, `rpc-trace.json`, `godot.log`, screenshots and diff images.

Exit codes are a stable contract:

| Code | Meaning |
|------|---------|
| 0 | success |
| 1 | internal error |
| 2 | usage: bad flags or an invalid scenario; nothing reached Godot |
| 3 | connection: could not launch, reach, or authenticate |
| 4 | Godot rejected a well-formed request |
| 5 | **an assertion or visual diff failed**: a real regression |
| 6 | timeout |

`godot-stagehand actions` lists every action a scenario step may use.

## Selectors

Target nodes using familiar patterns. Full syntax, matching rules and chaining
semantics: [docs/selectors.md](docs/selectors.md).

| Syntax | Example | Finds |
|--------|---------|-------|
| Path | `/root/UI/StartButton` | Node at exact path |
| Name | `name:*Button*` | Glob match on node name |
| Class | `class:Button` | All nodes of class |
| Group | `group:interactive` | All nodes in group |
| Text | `text:Start` | Nodes containing text |
| Meta | `meta:id=player` | Nodes with metadata |
| Role | `role:button` | Nodes with an accessibility role (Godot 4.5+) |
| Chain | `class:Panel >> name:*Btn*` | Scoped search (find within) |

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

**Read this before the setup steps below.** Once a game is running with
Stagehand enabled, anyone who has the session token can inspect and mutate its
state, including calling arbitrary methods if unsafe capabilities are opted
in. Treat it like any other local dev/debug port.

## Setup

The steps below use a terminal. Prefer not to? Copy the `addons/stagehand/`
folder into your project and enable it in **Project → Project Settings →
Plugins** (see [Quickstart, Step 2](docs/quickstart.md#step-2-install-the-addon)),
then use the **Setup…** button that appears in the editor toolbar. Its wizard
downloads the server binary, generates the MCP client config snippet, and
tests the connection to a running game — covering steps 1, 4, and part of 3
below without touching a terminal. Full walkthrough:
[Quickstart guide](docs/quickstart.md).

### 1. Get the server binary

**Download it** — no Go toolchain needed. Pick your platform from the
[latest release](https://github.com/mrf/godot-stagehand/releases/latest):

| Platform | File |
|----------|------|
| Linux x86-64 | `godot-stagehand-linux-amd64` |
| macOS Apple Silicon | `godot-stagehand-darwin-arm64` |
| macOS Intel | `godot-stagehand-darwin-amd64` |
| Windows x86-64 | `godot-stagehand-windows-amd64.exe` |

Or straight from the terminal (macOS/Linux — substitute your platform's file):

```bash
curl -fsSLo godot-stagehand \
  https://github.com/mrf/godot-stagehand/releases/latest/download/godot-stagehand-linux-amd64
chmod +x godot-stagehand
```

<details>
<summary><strong>Build from source</strong> (for contributors, or unsupported platforms)</summary>

Requires Go 1.25+ and Godot 4.3+:

```bash
go build -o godot-stagehand .
```

</details>

### 2. Install into your Godot project (one command)

```bash
godot-stagehand setup /path/to/your/godot/project
```

This copies the addon, enables the plugin, and registers the `StagehandServer`
autoload. It is idempotent, so it is safe to re-run. It then prints the MCP client
config snippet (with this binary's detected path) and the exact command to run
your game. Pass `--force` to overwrite an existing addon installation. On WSL it
also prints WSL-specific connection guidance.

> The old `./copy-addon.sh` script is deprecated; it now forwards to
> `godot-stagehand setup`.

### 3. Run your game with Stagehand enabled

```bash
# Your own project, or anywhere you control the argv
godot --path /path/to/your/project --stagehand

# Installing into an existing third-party project instead
STAGEHAND_ENABLED=1 godot --path /path/to/your/project
```

Prefer the environment variable when installing into a project you didn't
write. Many host projects (editors, tools, anything with its own `--help`)
parse their own command-line arguments and will reject `--stagehand` with
`Unknown option: --stagehand` and quit, even though Stagehand itself started
fine. `STAGEHAND_ENABLED=1` bypasses argument parsing entirely, so it works
regardless of what the host project's CLI parser recognizes.

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

**Optional: give your agent the skill.** [`skills/stagehand.md`](skills/stagehand.md)
is a ready-to-use skill file covering the tool workflow above. It ships in this
source tree (not in the binary release, since it's a prompt file, not a
runtime asset) — copy it into wherever your agent loads custom skills from.

## Configuration

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

## Godot version compatibility

**Minimum supported version: Godot 4.3.** Development happens against 4.6.x
locally; 4.3-4.7 are all tested and supported.

| Godot version | Status | Notes |
|----------------|--------|-------|
| 4.2 | **Not supported** | Addon fails to parse; not exercised by CI (see below) |
| 4.3 | Supported (minimum) | |
| 4.4 | Supported | |
| 4.5 | Supported | |
| 4.6 | Supported (local dev baseline) | |
| 4.7 | Supported | |

Verified by running the full connect-and-drive protocol (parse → activate →
authenticated ping → `get_tree`/`find_nodes`/`click`/`screenshot`) against a
real headless Godot binary of each version. See `scripts/test-godot-compat.sh`
and the `gdscript-parse` job in `.github/workflows/ci.yml`, which runs this
matrix on every push/PR to `main`.

### Known incompatibilities

- **Godot 4.2: two 4.3+ features the addon depends on.** Verified against a
  real `Godot_v4.2-stable_linux.x86_64` binary:
  1. **GDScript `is not`.** The addon uses `is not` (e.g. `stagehand_server.gd`,
     `input_recorder.gd`, `protocol/json_rpc.gd`) for readability. That operator
     was added in [godotengine/godot#87939](https://github.com/godotengine/godot/pull/87939),
     first released in Godot 4.3, so 4.2 fails at load with
     `Parse Error: Expected type specifier after "is"`.
  2. **`OS.get_entropy()`** (`addons/stagehand/autoload/stagehand_server.gd`),
     which generates the per-session auth token. Also 4.3+; on 4.2 it reports
     `Static function "get_entropy()" not found in base "GDScriptNativeClass"`.
     It surfaces only once the `is not` errors are cleared.

  Supporting 4.2 would mean rewriting those checks as `not (x is T)` and adding
  a `Crypto.generate_random_bytes()` fallback in the token path. 4.2 is treated
  as unsupported rather than carrying both for one older release, and CI does
  not run it. Every job in the compat matrix is blocking, so a red job means a
  real regression.

## Troubleshooting

**"Connection refused"**: Game isn't running with `--stagehand`, or wrong host/port.

**"Unknown option: --stagehand"**: The host project parses its own
command-line arguments and doesn't recognize `--stagehand`, so it aborts
before Stagehand (which had already started fine) is usable. Use
`STAGEHAND_ENABLED=1 godot --path /path/to/your/project` instead — it bypasses
argument parsing entirely. See [step 3](#3-run-your-game-with-stagehand-enabled) above.

**"Authentication required/failed"**: Pass the token printed by this Godot
session, or its configured `STAGEHAND_AUTH_TOKEN`, as
`godot_connect(auth_token=...)`. Generated tokens from prior runs do not work.

**"Connection reset"**: Godot started but `_process` isn't ticking (common in headless with heavy scenes). Use a visible window or a lighter scene.

**Screenshots are empty, black, or grey**: Visual workflows need a visible rendered window. Use `godot_launch(headless=false, expect_screenshots=true)`; headless launches are for structural tools.

**Port conflict**: Another instance on 26700. Set `STAGEHAND_PORT=26701`.

**Addon not in plugin list**: Run `godot-stagehand setup /path/to/project` again; it idempotently enables the plugin and autoload.

**Reading a failure**: Every failed call comes back as an error, never as a
successful-looking result, and its text names the method, the selector, a stable
machine-readable kind, and what to try next. The kinds and their JSON-RPC codes
are listed in the [error model](docs/error-model.md).

## Development

```bash
go vet ./...          # lint
go test ./...         # Go tests (no Godot needed)

# Scenario runner against a real headless Godot
GODOT_BIN=/path/to/godot go test -tags=godot -run '^TestScenarioRunner' .

# GDScript unit suite (GdUnit4, headless, needs Godot 4.6+)
GODOT_BIN=/path/to/godot ./scripts/run-gdscript-tests.sh
```

See the [GDScript testing guide](docs/gdscript-testing.md) for the suite layout
and the strict-mode rules test files must follow, the
[addon sync contract](docs/addon-sync-contract.md) before editing any copy of
the addon, and the [error model](docs/error-model.md) before adding a handler
that can fail.

## License

MIT. See [LICENSE](LICENSE).
