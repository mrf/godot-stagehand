# Godot Stagehand — Design Document

An MCP server + Godot addon that lets AI agents (and humans) automate and test running Godot games from outside the process, the way Playwright does for web browsers.

## Landscape: What Exists Today

| Tool | What it does | The gap |
|------|-------------|---------|
| **GdUnit4 / GUT** | In-process GDScript unit/integration testing | Runs *inside* Godot — no external control |
| **godot-mcp servers** (bradypp, ee0pdt, etc.) | Bridge AI assistants to the Godot *editor* | Controls the editor, not a running game |
| **Godot remote debugger** | TCP/WebSocket debug protocol (port 6007) | Used by editor, not designed for automation |
| **Godot 4.5+ AccessKit** | Screen reader support for Control nodes | No external API to query the accessibility tree |
| **Playwright MCP** | Full browser automation via Chrome DevTools Protocol | Nothing equivalent exists for game engines |

**The gap:** there is no way for an external process (Claude, a test runner, a CI pipeline) to connect to a running Godot game and interact with it programmatically — navigate scenes, click buttons, read node properties, take screenshots, wait for conditions.

## Architecture

```
                    MCP Protocol (JSON-RPC over stdio)
                    ===================================
Claude / AI Agent  <------>  godot-stagehand (Go binary)
                                      |
                                      | WebSocket (JSON-RPC 2.0)
                                      | ws://localhost:26700
                                      |
                              Running Godot Game
                              with stagehand addon
                              (GDScript WebSocket server)
```

Three layers:

1. **MCP Client** (Claude, any AI agent) — sends tool calls like `godot_click`, `godot_screenshot`, `godot_get_tree`
2. **Go MCP Server** (`godot-stagehand`) — translates MCP tool calls into Godot Wire Protocol messages over WebSocket
3. **Godot Addon** (`addons/stagehand/`) — GDScript WebSocket server embedded in the game, executes commands against the scene tree

The Go binary also has a CLI/scenario-runner frontend (`godot-stagehand run
scenario.json`, one-shot commands) for CI and terminal debugging without an
MCP client. It's a sibling of the MCP server sharing the same protocol-agnostic
core, not a replacement — see `docs/architecture/mcp-vs-cli.md`.

## Communication Protocol: Godot Wire Protocol (GWP)

JSON-RPC 2.0 over WebSocket. Every message is a standard JSON-RPC request/response.

### Request

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "query_nodes",
  "params": {
    "selector": "class:Button",
    "properties": ["text", "visible", "global_position"]
  }
}
```

### Response

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "nodes": [
      {
        "path": "/root/UI/StartButton",
        "class": "Button",
        "name": "StartButton",
        "properties": {
          "text": "Start Game",
          "visible": true,
          "global_position": {"x": 512, "y": 300}
        }
      }
    ]
  }
}
```

### GWP Methods

| Method | Purpose |
|--------|---------|
| `ping` | Handshake: engine info, versions, protocol version, capabilities |
| `get_tree` | Full scene tree snapshot |
| `query_nodes` | Find nodes matching a selector |
| `get_accessibility_tree` | Semantic, role-annotated view of the UI (derived, not real AccessKit — see below) |
| `get_property` | Read a property from a node |
| `set_property` | Write a property on a node |
| `call_method` | Call a method on a node |
| `evaluate` | Execute arbitrary GDScript expression |
| `change_scene` | Load a new scene |
| `screenshot` | Capture viewport as PNG base64 |
| `input_action` | Simulate an input action press/release |
| `input_mouse` | Simulate mouse click/move at coordinates |
| `input_mouse_move` | Move the mouse cursor without clicking |
| `input_key` | Simulate keyboard key press/release |
| `input_text` | Type text into the focused control |
| `input_touch` | Simulate a touch or drag |
| `focus_window` | Give a `Window` focus so key input reaches it |
| `wait_for_node` | Poll until a node exists / is visible / is removed |
| `wait_for_property` | Poll until a property satisfies a condition |
| `wait_signal` | Wait for a signal to be emitted (one-shot connection + timer, not polled) |
| `get_game_state` | Current scene, FPS, physics state, window size |
| `get_performance` | Read `Performance` singleton monitors |
| `assert_performance` | Assert a performance monitor against a threshold |
| `record_start` / `record_stop` | Start/stop recording input for record-and-replay |
| `replay` | Replay a recorded input session |

`godot_status`, `godot_list_instances`, `godot_disconnect`, `godot_screenshot_save_baseline`,
and `godot_screenshot_diff` are MCP-side only — they don't send a GWP method to the
addon (the latter two post-process a `screenshot` result locally in
`internal/visual`).

### Version and capability negotiation

`ping` doubles as the compatibility handshake. Alongside `engine_version` and
`stagehand_version` it reports `protocol_version` (an integer that must match
the client exactly), `protocol` (`gwp/1`), and a `capabilities` list naming the
method families the running addon will serve. Both `godot_launch` and
`godot_connect` negotiate before returning, so an incompatible pair is refused
at connect time rather than failing on a later call. Release versions may differ
across the pair; the protocol version may not. See `docs/versioning.md`.

## Selector System

Selectors are strings with a prefix-based grammar, inspired by Playwright's locators.

| Prefix | Example | Matches |
|--------|---------|---------|
| *(none)* | `"/root/UI/StartButton"` | Exact node path |
| `name:` | `"name:*Button*"` | Node name with glob matching |
| `class:` | `"class:Button"` | All nodes of a given Godot class |
| `group:` | `"group:interactive"` | All nodes in a group |
| `text:` | `"text:Start Game"` | Control nodes with matching text, substring + case-insensitive |
| `text=` | `"text=Start Game"` | Control nodes with exact (trimmed, case-sensitive) text |
| `meta:` | `"meta:pw_id=start_btn"` | Nodes with matching metadata |
| `unique:` | `"unique:StartButton"` | Nodes matching a unique-name-style identifier (tree-walk, not `%`-lookup — see below) |
| `role:` | `"role:button"` | Nodes whose derived accessibility role matches (case-insensitive) |

### Chaining

Multiple selectors can be chained with `>>` for scoping:

```
"class:Panel >> name:*Button*"
```

This finds all nodes named `*Button*` that are descendants of any `Panel`.

### GDScript Resolution

| Prefix | Implementation |
|--------|---------------|
| path | `get_node()` |
| `name:` | Recursive `find_child()` with pattern matching |
| `class:` | Tree walk + `is_class()` |
| `group:` | `get_nodes_in_group()` |
| `text:` / `text=` | Tree walk + check `text` property on Controls (substring vs. exact) |
| `meta:` | Tree walk + `has_meta()` / `get_meta()` |
| `unique:` | Tree walk matching a unique-name-style identifier — **not** `get_node("%"+name)`, since the addon can't assume the target scene assigned that node a scene-unique name |
| `role:` | Tree walk + derived accessibility role comparison (see `godot_get_accessibility_tree` below) |

## MCP Tool Set

31 tools are registered (`internal/mcpserver/server.go`); every tool below is
implemented. Every tool except `godot_connect`, `godot_launch`,
`godot_list_instances`, `godot_disconnect`, and `godot_status` also accepts an
optional `instance_id` (default `"default"`) — see "Multiple Instances" below.

### Navigation & Scene Management

**`godot_change_scene`**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `scene_path` | string | yes | Resource path, e.g. `"res://scenes/main_menu.tscn"` |

Returns: `{ success, current_scene }`

**`godot_get_game_state`**
No parameters. Returns: `{ current_scene, fps, physics_ticks, window_size, connected, engine_version }`

### Node Querying

**`godot_get_tree`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `root_path` | string | no | `"/root"` | Subtree root |
| `max_depth` | int | no | `10` | Recursion depth |
| `properties` | string[] | no | `[]` | Properties to include per node |

Returns: Recursive `{ name, class, path, children, properties }` tree.

**`godot_find_nodes`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `selector` | string | yes | | Selector expression |
| `properties` | string[] | no | `[]` | Properties to return per match |
| `limit` | int | no | `50` | Max results |

Returns: `{ nodes: NodeInfo[], count }`

**`godot_get_accessibility_tree`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `root_path` | string | no | `"/root"` | Subtree root |
| `max_depth` | int | no | `10` | Recursion depth |

Returns a role-annotated view (roles, names, values, states) tagged
`"source": "derived"`. Godot 4.5+'s AccessKit integration is a write-only push
API — GDScript cannot read the platform accessibility tree back, so this is
*derived* from the `Control` class hierarchy plus author-set
`accessibility_name`/`accessibility_description` and live focus/pressed/disabled
state, using the engine's own `DisplayServer.ROLE_*` vocabulary. See
`internal/mcpserver/tools_accessibility.go` and
`addons/stagehand/core/accessibility_tree.gd` for the verification notes.

### Property Access

**`godot_get_property`**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `selector` | string | yes | Target node |
| `property` | string | yes | Property name (supports dot notation: `"position.x"`) |

Returns: `{ value, type }`

**`godot_set_property`**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `selector` | string | yes | Target node |
| `property` | string | yes | Property name |
| `value` | any | yes | New value |

Returns: `{ success, previous_value }`

### Method Calling

**`godot_call_method`**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `selector` | string | yes | Target node |
| `method` | string | yes | Method name |
| `args` | any[] | no | Arguments |

The whole tool is gated: the addon rejects `call_method` outright unless
`STAGEHAND_ALLOW_UNSAFE=1` is set for the connected process. Even when
unsafe is allowed, private (`_`-prefixed) and a fixed set of destructive
methods (`free`, `queue_free`, `set_script`, `add_child`, …) stay blocked as
defense-in-depth — the same list (`internal/gwpop/method.go`) is enforced by
the MCP tools, the CLI, and the scenario runner.

Returns: `{ result }`

**`godot_evaluate`**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `expression` | string | yes | GDScript expression |
| `context_node` | string | no | Node path for `self` context |

Blocked unless the connected instance was launched with `allow_unsafe=true`.

Returns: `{ result, type }`

### Input Simulation

**`godot_click`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `selector` | string | no* | | Node to click (uses center) |
| `position` | `{x, y}` | no* | | Screen coordinates |
| `button` | string | no | `"left"` | `"left"`, `"right"`, `"middle"` |
| `double_click` | bool | no | `false` | |

*One of `selector` or `position` required.*

Returns: `{ clicked_at, node_path }`

**`godot_type_text`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `selector` | string | no | | Target input node |
| `text` | string | yes | | Text to type |
| `delay_ms` | int | no | `50` | Delay between keystrokes |

Returns: `{ success }`

**`godot_press_key`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `key` | string | yes | | Key name (`"Enter"`, `"Space"`, `"W"`, `"Escape"`) |
| `modifiers` | string[] | no | `[]` | `["shift", "ctrl", "alt", "meta"]` |
| `hold_ms` | int | no | `100` | Hold duration |

Returns: `{ success }`

**`godot_focus_window`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `selector` | string | no | | `Window` to focus; omit to target the modal subwindow that lost focus |

Returns: `{ success, window, auto_selected, already_focused }`

Key events are routed by the engine to whichever window holds focus, so a key
cannot be addressed at a named window — `godot_press_key` refuses with
`not_supported` when a visible modal has lost focus. This is the recovery. It is
a separate tool, not a `godot_press_key` parameter, because focusing mutates
application state the caller did not otherwise request.

**`godot_press_action`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `action` | string | yes | | Input action name (`"ui_accept"`, `"move_left"`) |
| `strength` | float | no | `1.0` | |
| `hold_ms` | int | no | `100` | |

Returns: `{ success }`

**`godot_mouse_move`**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `selector` | string | no* | Node to move to (uses center) |
| `coordinates` | `{x, y}` | no* | Target position |

*One of `selector` or `coordinates` required.* Returns: `{ success }`

**`godot_touch`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `position` | `{x, y}` | yes | | Touch start position |
| `index` | int | no | `0` | Finger index (0-9, multi-touch) |
| `action` | string | no | `"tap"` | `"tap"` (begin+end), `"begin"`, `"move"`, `"end"` |
| `drag_to` | `{x, y}` | no | | Drag destination; used by `"tap"` (swipe) and required by `"move"` |
| `duration_ms` | int | no | `100` | Delay before releasing the touch |

Returns: `{ success }`

### Visual Inspection

**`godot_screenshot`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `selector` | string | no | | Crop to this node's rect |
| `full_page` | bool | no | `true` | Ignored when `selector` is set |

Returns: MCP `ImageContent` (base64 PNG)

**`godot_screenshot_save_baseline`**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | yes | Baseline filename stem, e.g. `"main_menu"` -> `main_menu.png` |
| `selector` | string | no | Crop to this node's rect; use the same selector when diffing |

Saves under the server's baseline directory (default `stagehand-baselines`);
re-running with the same name overwrites it. Returns structured
`{ name, path, width, height }`.

**`godot_screenshot_diff`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `name` | string | yes | | Baseline name to compare against |
| `selector` | string | no | | Must match the baseline's selector |
| `threshold` | float | no | `0.0` | Max acceptable fraction of differing pixels `[0,1]` |
| `pixel_sensitivity` | float | no | `0.0` | Per-channel color tolerance `[0,1]` before a pixel counts as differing |

Returns structured `{ pass, diff_ratio, diff_pixels, max_delta, total_pixels,
width, height, baseline_path }`, plus `actual_image_path`/`diff_image_path` on
failure (written to the artifact directory, default `stagehand-diffs`). A
regression (`diff_ratio > threshold`) is reported as an MCP tool error.

### Performance

**`godot_get_performance`**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `monitors` | string[] | no | Monitor names (e.g. `TIME_FPS`, `MEMORY_STATIC`); a default set if omitted |

Returns: monitor name -> value map.

**`godot_assert_performance`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `monitor` | string | yes | | Monitor name |
| `threshold` | float | yes | | Value to compare against |
| `op` | string | no | `"lte"` | `"lt"`, `"lte"`, `"gt"`, `"gte"`, `"eq"` |

Returns structured `{ passed, monitor, value, threshold, op }`; a failed
assertion is reported as an MCP tool error.

### Recording

**`godot_record_start`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `output_path` | string | no | timestamped file under `user://stagehand_recordings/` | Where to save the recording |
| `include_mouse_move` | bool | no | `false` | Capture mouse-motion events too |

**`godot_record_stop`** — no parameters; stops the active recording and writes it to disk.

**`godot_replay`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `recording_path` | string | yes* | | Recording file to replay |
| `speed` | float | no | `1.0` | Playback speed multiplier (> 0) |
| `wait_for_ready` | bool | no | `true` | Wait for the current scene to finish loading first |

*`input_path` is accepted as a deprecated alias for `recording_path`.*

See `docs/recording-format.md`.

### Waiting

**`godot_wait_for_node`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `selector` | string | yes | | Node to wait for |
| `state` | string | no | `"exists"` | `"exists"`, `"visible"`, `"removed"` |
| `timeout_ms` | int | no | `10000` | 1–60000 |
| `poll_interval_ms` | int | no | `100` | 10–5000 |

Returns: `{ found, elapsed_ms, node }`

**`godot_wait_for_signal`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `selector` | string | yes | | Node that emits the signal |
| `signal_name` | string | yes | | Signal to wait for |
| `timeout_ms` | int | no | `5000` | 1–60000 |

Implemented as a one-shot signal connection with a timer-based timeout, not polling.

Returns: `{ received, elapsed_ms, args }`

**`godot_wait_for_property`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `selector` | string | yes | | Target node |
| `property` | string | yes | | Property to check |
| `operator` | string | yes | | `"equals"`, `"not_equals"`, `"exists"`, `"contains"`, `"greater_than"`, `"less_than"` |
| `expected_value` | any | yes unless `operator` is `"exists"` | | Value to compare against |
| `timeout_ms` | int | no | `10000` | 1–60000 |
| `poll_interval_ms` | int | no | `100` | 10–5000 |

Returns: `{ matched, elapsed_ms, actual_value }`

### Lifecycle

**`godot_connect`** — attach to an already-running game.
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `auth_token` | string | yes | | Session auth token (printed at addon startup, or `STAGEHAND_AUTH_TOKEN`) |
| `host` | string | no | `"localhost"` | |
| `port` | int | no | `26700` (shared default) | Required when `STAGEHAND_MULTI` is set |

Returns: `{ connected, engine_version, game_title }`. The addon's default port
is shared across every game on the machine; prefer `godot_launch`, which
starts a private instance on an auto-assigned port. See "Multiple Instances" below.

**`godot_launch`** — the paved road: start Godot and connect in one call.
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `project_path` | string | yes | | Path to the Godot project directory |
| `godot_bin` | string | no | resolved via env/PATH | Path to the Godot binary |
| `host` | string | no | `"127.0.0.1"` | |
| `port` | int | no | `0` (auto-assign) | `0` picks a free port so the instance stays private |
| `headless` | bool | no | `true` | |
| `expect_screenshots` | bool | no | `false` | Rejects `headless=true` (screenshots need a visible window) |
| `allow_unsafe` | bool | no | `false` | Enables `godot_evaluate` / unrestricted `godot_call_method` for this instance |
| `share_user_data` | bool | no | `false` | Use the project's real `user://` instead of a private, per-launch one |
| `extra_args` | string[] | no | `[]` | Extra Godot CLI args |
| `timeout_ms` | int | no | `30000` | Max time to wait for Godot to become ready |

Auto-assigned ports retry up to 3 times on a lost port race
(`maxAutoPortAttempts` in `tools_launch.go`). Returns `{ instance_id, pid,
host, port, engine_version, stagehand_version, unsafe_methods_enabled,
user_data_dir, connection_guidance, protocol, capabilities, warnings? }`.

### Instance Management

**`godot_list_instances`** — no parameters. Returns `{ instances: [{ id, host, port, pid, connected }] }` for every connection this process manages.

**`godot_disconnect`**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `instance_id` | string | yes | ID of the instance to disconnect and remove |

**`godot_status`** — no parameters. Human-readable summary of every managed instance (connection state, address, PID, reconnect-exhausted note).

## GDScript Addon Structure

```
addons/stagehand/
  plugin.cfg                    # Editor plugin metadata
  plugin.gd                     # Editor plugin: toolbar toggle + setup wizard
  stagehand_version.gd          # Addon version constant
  autoload/
    stagehand_server.gd         # WebSocket server — the core autoload
  core/
    command_router.gd           # Routes JSON-RPC methods to handlers
    errors.gd                   # Canonical {error, error_code, details} envelope
    selector_engine.gd          # Parses and resolves selector expressions
    tree_serializer.gd          # Serializes scene tree to JSON-safe dicts
    accessibility_tree.gd       # Derived accessibility-role tree (see above)
    screenshot_capture.gd       # Viewport capture + base64 encoding
    input_simulator.gd          # Synthesizes InputEvent objects
    input_recorder.gd           # Input recording + replay
    waiter.gd                   # Wait-for-condition polling + signal waiting
    expression_evaluator.gd     # Evaluates GDScript expressions
    method_handler.gd           # call_method dispatch + blocked-method checks
    property_handler.gd         # get_property/set_property, dot-path resolution
    scene_handler.gd            # change_scene
  editor/
    setup_panel.gd              # First-run setup wizard dock
    release_assets.gd           # Release-export asset handling
  protocol/
    json_rpc.gd                 # JSON-RPC 2.0 message parsing/construction
```

Cross-file references inside `core/` use `const X := preload("res://...")`,
never bare `class_name` — the headless global class cache is empty, so a
bare-name reference resolves at runtime but not under `--headless`.

### Activation Guard

The WebSocket server only starts when explicitly enabled, via any of:

- Environment variable `STAGEHAND_ENABLED=1`
- Command-line flag `--stagehand`
- The editor toolbar toggle (persisted in editor-only metadata) or setup
  wizard, which inject `--stagehand` into editor play sessions

None of these persist runtime activation in `project.godot`; the autoload
checks on `_ready()` and disables itself otherwise. Release exports
additionally require the deliberate `STAGEHAND_ALLOW_RELEASE=1` unsafe opt-in.
A session auth token gates every non-`ping` method once connected (see
`godot_connect` above). `godot_evaluate` and unrestricted `godot_call_method`
additionally require the addon process itself to have `STAGEHAND_ALLOW_UNSAFE=1`
set — `godot_launch`'s `allow_unsafe` parameter sets this env var for the child
process it starts; a game you attach to via `godot_connect` must already have
been started with it set.

### Screenshot Capture

```gdscript
static func capture(viewport: Viewport, rect: Rect2i = Rect2i()) -> String:
    await RenderingServer.frame_post_draw
    var img := viewport.get_texture().get_image()
    if rect != Rect2i():
        img = img.get_region(rect)
    var buffer := img.save_png_to_buffer()
    return Marshalls.raw_to_base64(buffer)
```

Screenshot requests are async — the response is sent after the frame is drawn, not inline with the WebSocket message handler.

### Wait-for-Condition

Polling approach in `_process()` (~16ms at 60fps):

```gdscript
func _process(_delta: float) -> void:
    var now := Time.get_ticks_msec()
    for wait in _pending_waits:
        var elapsed := now - wait.start_ms
        if wait.check_fn.call():
            _send_response(wait.id, {matched = true, elapsed_ms = elapsed})
            _remove_wait(wait)
        elif elapsed > wait.timeout_ms:
            _send_response(wait.id, {matched = false, reason = "timeout"})
            _remove_wait(wait)
```

For `wait_for_signal`, a one-shot signal connection with a timer-based timeout is used instead of polling.

## Go MCP Server Structure

```
godot-stagehand/
  go.mod
  go.sum
  main.go                        # No args = MCP stdio server; any arg dispatches
                                  # to setup / --version / internal/cli
  internal/
    mcpserver/
      server.go                  # MCP server setup, tool registration, instance plumbing
      instance_manager.go        # Named-instance registry (instance_id -> connection)
      tools_lifecycle.go         # godot_connect, godot_get_game_state
      tools_launch.go            # godot_launch (+ auto-port retry)
      tools_instances.go         # godot_list_instances, godot_disconnect
      tools_status.go            # godot_status
      tools_query.go             # godot_get_tree, godot_find_nodes
      tools_accessibility.go     # godot_get_accessibility_tree
      tools_property.go          # godot_get_property, godot_set_property
      tools_method.go            # godot_call_method, godot_evaluate, godot_change_scene
      tools_input.go             # godot_click, godot_press_key, godot_type_text, etc.
      tools_visual.go            # godot_screenshot, save_baseline, screenshot_diff
      tools_wait.go              # godot_wait_for_*
      tools_performance.go       # godot_get_performance, godot_assert_performance
      tools_record.go            # godot_record_start/stop, godot_replay
      tools_connection_guidance.go # Shared "you may be sharing another agent's game" copy
    cli/                          # One-shot commands + `run <scenario>` (sibling of mcpserver)
    scenario/                     # Scenario model, validation, runner, JSON/JUnit/trace reporters
    gwpop/                        # Action registry shared by cli/scenario: action -> GWP method
    gwp/                          # Protocol version, capabilities, handshake, error rendering
    visual/                       # Screenshot decode, baselines, pixel diff (shared by mcpserver + scenario)
    godotconn/
      conn.go                    # WebSocket connection to Godot addon
      reconnect.go               # Reconnection with exponential backoff
      protocol.go                # GWP message types, JSON-RPC helpers
    launch/                      # Process management: launch, kill, port/user-data isolation
    selector/
      parse.go                   # Selector parsing and validation
      parse_test.go
    setup/                       # `godot-stagehand setup` CLI subcommand
    version/                     # Authoritative version constants
  testdata/
    test_project/                # Minimal Godot project for integration tests
      project.godot
      addons/stagehand/          # The addon copy under test (see addon-sync-contract.md)
      scenes/
  addons/
    stagehand/                   # The ONLY authoritative GDScript addon copy
  examples/
    minimal-game/addons/stagehand/ # Distributed copy kept in sync per addon-sync-contract.md
```

`cli` and `mcpserver` are siblings — neither imports the other; `mcpserver`
does not route through `gwpop`. See `docs/architecture/mcp-vs-cli.md` for why,
and `docs/cli.md` for the CLI/scenario runner itself.

### Connection Multiplexing

The Go WebSocket connection multiplexes concurrent requests. Each in-flight JSON-RPC call gets a unique ID and its own response channel:

```go
type Connection struct {
    ws       *websocket.Conn
    nextID   atomic.Int64
    pending  map[int64]chan *Response
}

func (c *Connection) Call(ctx context.Context, method string, params any) (*Response, error) {
    id := c.nextID.Add(1)
    ch := make(chan *Response, 1)
    c.pending[id] = ch
    c.send(Request{ID: id, Method: method, Params: params})
    select {
    case resp := <-ch:
        return resp, nil
    case <-ctx.Done():
        return nil, ctx.Err()
    }
}
```

## Connection Lifecycle

### States

1. **Disconnected** — No Godot game running or addon not reachable
2. **Connecting** — Attempting WebSocket handshake
3. **Connected** — Active connection, commands can be sent
4. **Reconnecting** — Connection lost, exponential backoff (100ms → 5s cap)

A connection that exhausts reconnection gives up permanently
(`Connection.giveUp`, `internal/godotconn/reconnect.go`) rather than retrying
forever; `godot_status` surfaces this as "gave up reconnecting" so a dead
instance doesn't look like it's still trying.

### Startup Flow

1. MCP server starts, listens on stdio
2. Agent calls `godot_launch` (start a private Godot instance — the paved
   road) or `godot_connect` (attach to a game already running)
3. If launching: Go server starts Godot with `--stagehand` (plus
   `STAGEHAND_INSTANCE_TOKEN` and, when requested, `STAGEHAND_ALLOW_UNSAFE`),
   polls the WebSocket until ready, and negotiates the GWP handshake
4. If connecting: caller supplies `auth_token`; Go server dials, authenticates,
   pings, and negotiates the handshake
5. All subsequent tool calls for that `instance_id` use this connection

### Error Handling

- Tools called before connection return `isError: true` with guidance to call
  `godot_connect` or `godot_launch` first
- On reconnect, pending waits are cancelled

### Multiple Instances

One Go MCP server process can hold several *named* connections at once —
this is a deviation from the original one-process-one-instance design.
Every tool (except `godot_connect`/`godot_launch`/`godot_list_instances`/
`godot_disconnect`/`godot_status`) takes an optional `instance_id` (default
`"default"`) routed through `instanceManager` (`internal/mcpserver/instance_manager.go`),
an in-process `map[string]*instanceEntry` guarded by a mutex.

That registry is **per-process, not shared**. The real multi-instance
scenario — N subagents, each its own MCP client — is N separate
`godot-stagehand` processes, which coordinate only through port numbers and
the per-launch instance token:

- `godot_launch` with the default `port=0` auto-assigns a free port (retried
  up to 3 times on a lost TOCTOU race), so each launched instance is private
  by construction.
- `godot_connect`'s default port (26700) is shared — two agents connecting
  with defaults attach to the *same* game and mutate the same `SceneTree`.
  Setting `STAGEHAND_MULTI` forces every `godot_connect` call to pass an
  explicit `port`, and tool descriptions steer agents toward `godot_launch`.
- Each `godot_launch` gets an isolated `user://` by default (`share_user_data`
  opts out) so concurrent instances of the *same* project can't corrupt each
  other's saves; the shared `res://.godot` import cache is protected by a
  cross-process locked import-once step. See
  `docs/architecture/instance-isolation.md` for the full contract and why
  Godot 4 has no `--user-data-dir` flag to rely on instead.
- A per-launch token (`STAGEHAND_INSTANCE_TOKEN`) proves the process a
  launcher connected to is the one it spawned, rejecting port squatters.

There is deliberately no cross-process registry beyond that — see
`docs/audits/2026-07-08-implementation-audit.md` §3 for the open gaps
(untested concurrent-launch races, no coordination layer).

## Testing Strategy

### Unit Tests (Go)

- Selector parsing (`selector/parse_test.go`)
- JSON-RPC message construction
- Connection state machine with mock WebSocket

### Unit Tests (GDScript)

Using GdUnit4:
- `SelectorEngine` — each selector type against a mock scene tree
- `TreeSerializer` — serialization of various node types
- `InputSimulator` — correct InputEvent construction
- `JsonRpc` — message parsing/construction

### Integration Tests

Minimal Godot project in `testdata/test_project/`. Go tests launch Godot headlessly, connect via WebSocket, exercise each GWP method:

```go
func TestIntegration_GetTree(t *testing.T) {
    result, _ := launch.Launch(ctx, launch.Config{
        ProjectPath: "testdata/test_project",
        Headless:    true,
    })
    defer result.Conn.Close()
    resp, _ := result.Conn.Call(ctx, "get_tree", map[string]any{"max_depth": 3})
    assert.Equal(t, "root", resp.Result.Tree.Name)
}
```

### End-to-End MCP Tests

In-memory MCP transports with a mock Godot WebSocket server to test the full Claude → MCP → GWP → response flow.

## Implementation Status

Every capability the phased plan called for is built, with two deliberate
exceptions:

- **Real AccessKit read-back** — impossible from GDScript (write-only API);
  `godot_get_accessibility_tree` ships a derived equivalent instead (see above).
- **Cross-process instance coordination** — each MCP process still only
  coordinates with its own launched instances via port + token, not with
  sibling processes (see "Multiple Instances" above).

Everything else — `godot_launch`, `godot_wait_for_node`/`_signal`/`_property`,
`godot_call_method`, `godot_evaluate`, `godot_change_scene`, `godot_type_text`,
`godot_mouse_move`, `godot_touch`, all seven selector prefixes with `>>`
chaining, screenshot baselines/diffing, performance monitors/assertions,
record-and-replay, and GitHub Actions CI (`.github/workflows/ci.yml`,
`release.yml`) — is implemented. The project has also grown two things the
original design didn't anticipate: a CLI + scenario runner
(`internal/cli`, `internal/scenario` — see `docs/architecture/mcp-vs-cli.md`
and `docs/cli.md`) and the multi-instance-per-process architecture described
above. See `docs/audits/2026-07-08-implementation-audit.md` for the full
conformance audit this section is based on, plus open stability gaps not yet
addressed.

## Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| **WebSocket over TCP** | Provides framing (no length-prefix needed), well-supported in Go (`gorilla/websocket`) and GDScript (`WebSocketPeer`), works through proxies |
| **JSON-RPC 2.0** | Same protocol MCP uses — consistent, `id`-based correlation for multiplexing |
| **Polling for waits** | Simpler than signal-based. At 60fps, 16ms polling is fast enough for automation |
| **Go for MCP server** | User's preferred backend language, strong concurrency primitives, single binary distribution |
| **Port 26700** | Distinct from Godot's debugger (6007). Configurable via flag/env var |
| **Autoload over editor-plugin-only** | Addon must run inside the *game* process, not just the editor. Autoload is the simplest mechanism |
| **Env var activation guard** | `STAGEHAND_ENABLED=1` or `--stagehand` flag; release exports also require `STAGEHAND_ALLOW_RELEASE=1` |
| **Selector prefix grammar** | Inspired by Playwright's locator strategies but adapted for Godot's node tree (paths, groups, classes instead of CSS/ARIA) |
| **Named instances over one-process-one-instance** | A single MCP process managing multiple `instance_id`s (rather than one connection per process) lets one agent drive several games without spawning several `godot-stagehand` binaries; real multi-agent isolation still comes from separate processes plus port/token, not from this |
| **`godot_launch` as the paved road, `godot_connect` as the escape hatch** | Auto-assigned ports make accidental cross-agent SceneTree sharing structurally unlikely for the common case; `godot_connect`'s shared default port is kept for attaching to a game a human already started |
| **Auth token over unauthenticated localhost** | Any localhost peer can otherwise execute arbitrary GDScript; a per-session token (printed at startup or `STAGEHAND_AUTH_TOKEN`) is a low-cost gate, not a real security boundary |

## Security Model / Threat Model

Stagehand's WebSocket server is designed for local development and test
automation, not for exposure to untrusted networks or processes:

- **Any peer that reaches the port can execute arbitrary GDScript** with the
  full privileges of the running game process (`godot_evaluate`,
  `godot_call_method`, and friends are unrestricted code execution by design,
  not a bug). The `auth_token` gate (see above) stops a *casual* second
  connection, such as another agent guessing the shared default port — it
  does not stop a co-resident process that can read the token from the
  addon's stdout, the environment, or process memory. Treat it as a
  collision-avoidance mechanism, not an authentication boundary.
- **The server binds to `127.0.0.1` by default and is meant to stay there.**
  Anything that makes the port reachable from outside localhost (port
  forwarding, binding `0.0.0.0`, exposing it through a container/VM without a
  firewall) turns "any local peer" into "any network peer" for the same
  arbitrary-code-execution surface.
- **Release exports require an explicit opt-in** (`STAGEHAND_ALLOW_RELEASE=1`)
  precisely because shipping this listener in a distributed build would hand
  arbitrary code execution to anyone who can reach the running game.
- **Scope:** this is an accepted risk for the tool's intended use (a
  developer or CI job automating their own local/headless Godot instance),
  not a gap to "fix" with stronger auth — if the trust model changes (e.g.
  remote automation across a network), revisit this section first.

## Current Troubleshooting Guide

### Common Issues and Solutions:

1. **Connection fails immediately after connecting:**
   - Ensure the Godot project has the stagehand addon installed and enabled
   - Verify the addon is enabled in the Godot editor plugins panel
   - Make sure you launched Godot with `STAGEHAND_ENABLED=1` or `--stagehand` flag
   - Check that no other apps are using port 26700 (default) or specified port

2. **Addon not detected or "not enabled" error:**
   - Verify the `addons/stagehand/` folder structure exists in your Godot project
   - Check that the addon is activated in Project Settings > Plugins
   - Ensure you're running Godot with the environment variable or flag to enable it

3. **Headless Godot doesn't work as expected:**
   - Godot Stagehand works best with visible GUI elements in headed mode
   - Some input simulations may behave unexpectedly in headless mode
   - Recommended: primarily test with GUI-enabled Godot sessions

4. **Port conflict with multiple instances:**
   - By default, Godot Stagehand uses port 26700
   - Use environment variable `STAGEHAND_PORT=XXXX` to specify different port
   - Or use command line, with the flag after the `--` separator: `godot ... -- --stagehand --stagehand-port=XXXX`

5. **No response when using automation commands:**
   - Ensure scene is loaded before attempting automation
   - Verify target nodes exist before referencing them
   - Check Godot console for errors (the addon prints server status messages)
