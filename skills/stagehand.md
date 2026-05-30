---
name: stagehand
description: Launch, connect, and interact with Godot games via the godot-stagehand MCP server. Handles game launch, visual inspection, gameplay interaction, automated testing, and input recording/replay. Use when you need to automate a Godot game, test UI interactions, verify visual changes, or run gameplay sequences.
arguments:
  - name: action
    description: "What to do: 'launch' (start game + connect), 'connect' (attach to running game), 'screenshot' (capture current state), 'explore' (tree + screenshot to understand current state), 'play' (describe a gameplay sequence to execute), 'stop' (kill the Godot process)"
    required: false
  - name: detail
    description: "Additional context for the action — e.g., what to look at, what sequence to play, which node to inspect"
    required: false
---

# Godot Stagehand — Game Automation Skill

You are an expert at using the godot-stagehand MCP tools to launch, observe, and interact with Godot games. This skill teaches you how to automate any Godot game that has the Stagehand addon installed.

## Architecture

```
Claude --stdio--> godot-stagehand (Go MCP server) --WebSocket:26700--> Godot Game
                                                                        └── StagehandServer autoload
                                                                             └── JSON-RPC command router
```

The MCP server runs as a child process of Claude Code (configured in `.mcp.json` or `~/.claude/.mcp.json`). It connects to the Godot game's WebSocket server on port 26700 (configurable). The Stagehand addon runs inside Godot as an autoload singleton.

## Prerequisites

- Godot game must have the `addons/stagehand/` addon installed and enabled
- Game must be launched with `--stagehand` CLI flag OR `STAGEHAND_ENABLED=1` environment variable
- The `godot-stagehand` MCP server must be configured in Claude Code's MCP settings

## Tool Reference

### Connection & Lifecycle

#### `godot_launch`
Launch a Godot game with stagehand enabled and connect to it.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `project_path` | yes | — | Path to the Godot project directory (contains project.godot) |
| `godot_bin` | no | auto-detect | Path to the Godot binary |
| `host` | no | `127.0.0.1` | WebSocket host |
| `port` | no | `26700` | TCP port for the WebSocket server |
| `headless` | no | `true` | Launch Godot in headless mode |
| `expect_screenshots` | no | `false` | Reject `headless=true` for screenshot/baseline/diff workflows |
| `extra_args` | no | `[]` | Extra command-line arguments for the Godot binary |
| `timeout_ms` | no | `30000` | Maximum time to wait for Godot to start (min: 1000) |

```
godot_launch(project_path="/path/to/project", headless=false, timeout_ms=45000)
```

#### `godot_connect`
Connect to an already-running Godot game with stagehand enabled.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `host` | no | `127.0.0.1` | WebSocket host |
| `port` | no | `26700` | WebSocket port |

```
godot_connect(host="127.0.0.1", port=26700)
```

> **Host selection:** use `127.0.0.1` when Godot and `godot-stagehand` run in
> the same OS/network namespace, including Linux Godot inside WSL. If Godot runs
> as a *Windows* binary and the MCP client runs in *WSL*, use `localhost` only
> under WSL **mirrored** networking (`networkingMode=mirrored`). On a NAT/default
> WSL network, dial the **default-gateway IP** instead (it is the Windows host
> and changes across reboots):
> ```bash
> ip route show default | awk '/default/ {print $3}'   # -> host for godot_connect
> ```

#### `godot_get_game_state`
Get current game state: scene name, FPS, physics state, window size. No parameters.

```
godot_get_game_state()
```

#### `godot_change_scene`
Change to a different scene in the running game.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `scene_path` | yes | — | Resource path (e.g. `res://scenes/main_menu.tscn`) |

```
godot_change_scene(scene_path="res://scenes/main_menu.tscn")
```

### Scene Tree Queries

#### `godot_get_tree`
Get a snapshot of the Godot scene tree.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `root_path` | no | `/root` | Subtree root path |
| `max_depth` | no | `10` | Maximum recursion depth (1-50) |
| `include_properties` | no | `[]` | Property names to include per node |

```
godot_get_tree(max_depth=3)
godot_get_tree(root_path="/root/Main/UI", include_properties=["visible", "modulate"])
```

#### `godot_find_nodes`
Find nodes matching a selector expression.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `selector` | yes | — | Selector expression (see Selector Syntax below) |
| `properties` | no | `[]` | Property names to return per matched node |
| `limit` | no | `50` | Maximum results (1-500) |

```
godot_find_nodes(selector="class:Button", properties=["text", "visible"])
godot_find_nodes(selector="name:*Player*")
godot_find_nodes(selector="class:Panel >> text:Submit")
```

### Properties

#### `godot_get_property`
Read a property from a node (supports dot notation like `position.x`).

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `selector` | yes | — | Target node selector |
| `property` | yes | — | Property name (supports dot notation) |

```
godot_get_property(selector="class:Player", property="position")
godot_get_property(selector="name:HealthBar", property="value")
godot_get_property(selector="/root/Main/Score", property="text")
```

#### `godot_set_property`
Set a property on a node.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `selector` | yes | — | Target node selector |
| `property` | yes | — | Property name |
| `value` | yes | — | New property value |

```
godot_set_property(selector="name:Player", property="position", value={"x": 100, "y": 200})
godot_set_property(selector="class:Label", property="text", value="Hello World")
```

### Input Simulation

#### `godot_click`
Click on a node or at screen coordinates.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `selector` | no* | — | Node to click |
| `position` | no* | — | Screen coordinates `{x, y}` |
| `button` | no | `left` | Mouse button: `left`, `right`, `middle` |
| `double_click` | no | `false` | Whether to double-click |

*One of `selector` or `position` is required.

```
godot_click(selector="text:Start Game")
godot_click(selector="class:Button >> text:OK")
godot_click(position={"x": 400, "y": 300}, button="right")
```

#### `godot_press_key`
Simulate a keyboard key press.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `key` | yes | — | Key name (e.g. `Enter`, `Space`, `W`, `Escape`) |
| `modifiers` | no | `[]` | Modifier keys: `shift`, `ctrl`, `alt`, `meta` |
| `hold_ms` | no | `100` | How long to hold the key in milliseconds |

```
godot_press_key(key="Enter")
godot_press_key(key="S", modifiers=["ctrl"])
godot_press_key(key="Space", hold_ms=2000)
```

#### `godot_press_action`
Simulate a Godot input action (as defined in the project's Input Map).

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `action` | yes | — | Input action name (e.g. `ui_accept`, `move_left`) |
| `strength` | no | `1.0` | Action strength (0.0-1.0) |
| `hold_ms` | no | `100` | How long to hold the action in milliseconds |

```
godot_press_action(action="ui_accept")
godot_press_action(action="move_right", hold_ms=2000)
godot_press_action(action="jump", strength=0.5)
```

#### `godot_type_text`
Send text input to the focused control.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `text` | yes | — | Text to type |
| `delay_ms` | no | `50` | Delay between characters in milliseconds |
| `selector` | no | — | Optional node to click first to gain focus |

```
godot_type_text(text="player_name", selector="class:LineEdit")
godot_type_text(text="Hello World")
```

#### `godot_mouse_move`
Move mouse cursor without clicking.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `selector` | no* | — | Node whose center to move to |
| `coordinates` | no* | — | Absolute screen coordinates `{x, y}` |

*One of `selector` or `coordinates` is required.

```
godot_mouse_move(selector="class:Button")
godot_mouse_move(coordinates={"x": 200, "y": 150})
```

#### `godot_touch`
Simulate touch screen events.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `position` | yes | — | Screen coordinates `{x, y}` for touch start |
| `index` | no | `0` | Touch finger index (0-9 for multi-touch) |
| `action` | no | `tap` | Touch action: `tap`, `begin`, `move`, `end` |
| `drag_to` | no | — | Drag destination coordinates `{x, y}` |
| `duration_ms` | no | `100` | Duration before releasing |

```
godot_touch(position={"x": 400, "y": 300})
godot_touch(position={"x": 100, "y": 200}, drag_to={"x": 300, "y": 200}, action="tap")
```

### Visual Testing

#### `godot_screenshot`
Capture a screenshot of the game viewport.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `selector` | no | — | Crop to this node's bounding rect |
| `full_page` | no | `true` | Capture the full viewport |

```
godot_screenshot()
godot_screenshot(selector="class:InventoryPanel")
```

#### `godot_screenshot_save_baseline`
Capture a screenshot and save it as a named baseline for future comparison.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `name` | yes | — | Baseline name (used as filename) |
| `selector` | no | — | Crop to this node's bounding rect |

```
godot_screenshot_save_baseline(name="main_menu")
godot_screenshot_save_baseline(name="inventory_open", selector="class:InventoryPanel")
```

#### `godot_screenshot_diff`
Compare current screenshot against a saved baseline.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `name` | yes | — | Baseline name to compare against |
| `selector` | no | — | Crop to this node's bounding rect |
| `threshold` | no | `0.0` | Max acceptable fraction of differing pixels (0.0-1.0) |
| `pixel_sensitivity` | no | `0.0` | Per-pixel color delta tolerance (0.0-1.0) |

```
godot_screenshot_diff(name="main_menu")
godot_screenshot_diff(name="hud", threshold=0.01, pixel_sensitivity=0.05)
```

### Waiting / Synchronization

#### `godot_wait_for_node`
Wait for a node matching a selector to reach a desired state.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `selector` | yes | — | Selector for the node to wait for |
| `state` | no | `exists` | State to wait for: `exists`, `visible`, `removed` |
| `timeout_ms` | no | `10000` | Maximum wait time (1-60000) |
| `poll_interval_ms` | no | `100` | Polling interval (10-5000) |

```
godot_wait_for_node(selector="class:MainMenu", state="visible")
godot_wait_for_node(selector="class:LoadingScreen", state="removed", timeout_ms=30000)
```

#### `godot_wait_for_signal`
Wait for a specific signal to be emitted on a node.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `selector` | yes | — | Selector for the node that emits the signal |
| `signal_name` | yes | — | Name of the signal to wait for |
| `timeout_ms` | no | `5000` | Maximum wait time (1-60000) |

```
godot_wait_for_signal(selector="class:AnimationPlayer", signal_name="animation_finished")
godot_wait_for_signal(selector="/root/Main", signal_name="level_loaded", timeout_ms=15000)
```

#### `godot_wait_for_property`
Wait for a node's property to satisfy a condition.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `selector` | yes | — | Selector for the node |
| `property` | yes | — | Property name to monitor |
| `operator` | yes | — | Comparison: `equals`, `not_equals`, `exists`, `contains`, `greater_than`, `less_than` |
| `expected_value` | no* | — | Value to compare against (*required unless operator is `exists`) |
| `timeout_ms` | no | `10000` | Maximum wait time (1-60000) |
| `poll_interval_ms` | no | `100` | Polling interval (10-5000) |

```
godot_wait_for_property(selector="class:HealthBar", property="value", operator="less_than", expected_value=50)
godot_wait_for_property(selector="name:Player", property="is_grounded", operator="equals", expected_value=true)
```

### Method Calls & Evaluation

#### `godot_call_method`
Call a method on a node. Some destructive/private methods are blocked for safety.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `selector` | yes | — | Target node selector |
| `method` | yes | — | Method name to call |
| `args` | no | `[]` | Arguments to pass |
| `allow_multiple` | no | `false` | Allow calling on multiple matched nodes |

Blocked methods: `free`, `queue_free`, `set_script`, `add_child`, `remove_child`, `queue_redraw`, `notification`, `propagate_notification`, `set_process`, `set_physics_process`, and any method starting with `_`.

```
godot_call_method(selector="class:AudioManager", method="play_sfx", args=["click"])
godot_call_method(selector="name:Enemy", method="take_damage", args=[25])
```

#### `godot_evaluate`
Evaluate a GDScript expression in the running game. Powerful but dangerous.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `expression` | yes | — | GDScript expression to evaluate |
| `context_node` | no | — | Optional node path to use as `self` context |

```
godot_evaluate(expression="get_tree().current_scene.name")
godot_evaluate(expression="Engine.get_frames_per_second()")
godot_evaluate(expression="score", context_node="/root/Main/GameManager")
```

### Performance Monitoring

#### `godot_get_performance`
Get performance metrics from the Godot Performance singleton.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `monitors` | no | default set | Performance monitor names (e.g. `TIME_FPS`, `MEMORY_STATIC`) |

```
godot_get_performance()
godot_get_performance(monitors=["TIME_FPS", "OBJECT_COUNT", "RENDER_TOTAL_DRAW_CALLS_IN_FRAME"])
```

#### `godot_assert_performance`
Assert that a performance monitor meets a threshold. Returns pass/fail.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `monitor` | yes | — | Performance monitor name |
| `threshold` | yes | — | Threshold value |
| `op` | no | `lte` | Comparison: `lt`, `lte`, `gt`, `gte`, `eq` |

```
godot_assert_performance(monitor="TIME_FPS", threshold=30, op="gte")
godot_assert_performance(monitor="MEMORY_STATIC", threshold=536870912, op="lte")
```

### Input Recording & Replay

#### `godot_record_start`
Start recording player input.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `output_path` | yes | — | File path for the recording (e.g. `res://recordings/run1.json`) |

```
godot_record_start(output_path="res://recordings/test_run.json")
```

#### `godot_record_stop`
Stop an active recording. No parameters.

```
godot_record_stop()
```

#### `godot_replay`
Replay a previously recorded input session.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `input_path` | yes | — | File path of the recording to replay |

```
godot_replay(input_path="res://recordings/test_run.json")
```

## Selector Syntax

Selectors identify nodes in the Godot scene tree. They are validated on the Go side before being sent to the addon.

### Selector Types

| Prefix | Description | Example |
|--------|-------------|---------|
| *(none)* | Exact node path | `/root/Main/Player` |
| `name:` | Match by node name (supports `*` wildcards) | `name:*Player*` |
| `class:` | Match by class/type | `class:Button` |
| `group:` | Match by group membership | `group:enemies` |
| `text:` | Match by text content (Labels, Buttons) | `text:Submit` |
| `meta:` | Match by metadata attribute | `meta:id=submit_btn` |
| `unique:` | Match by unique identifier | `unique:main-menu-play` |

### Chaining with `>>`

Chain selectors to narrow scope. Each segment filters within the results of the previous one:

```
class:Panel >> text:OK           # Find "OK" text inside any Panel
name:Inventory >> class:Button   # Find Buttons inside the Inventory node
group:ui >> class:Label >> text:Score  # Label with "Score" text, in a node in the "ui" group
```

### Wildcard Patterns

The `name:` selector supports `*` wildcards:

```
name:*Player*     # Any node with "Player" in the name
name:Enemy_*      # Nodes starting with "Enemy_"
name:*_Label      # Nodes ending with "_Label"
```

## Common Workflow Patterns

### Launch and Verify

```
1. godot_launch(project_path="/path/to/project", headless=false, timeout_ms=45000)
2. godot_get_game_state()          # Confirm scene, FPS, window
3. godot_screenshot()              # Visual confirmation
```

### Navigate a Menu

```
1. godot_wait_for_node(selector="class:MainMenu", state="visible")
2. godot_screenshot()
3. godot_click(selector="text:Play")
4. godot_wait_for_node(selector="class:MainMenu", state="removed")
5. godot_wait_for_node(selector="class:GameWorld", state="visible")
6. godot_screenshot()
```

### Click a Button and Wait for Result

```
1. godot_click(selector="class:Dialog >> text:Confirm")
2. godot_wait_for_node(selector="class:Dialog", state="removed")
3. godot_screenshot()
```

### Fill a Form

```
1. godot_type_text(text="player1", selector="name:UsernameField")
2. godot_type_text(text="password123", selector="name:PasswordField")
3. godot_click(selector="text:Login")
4. godot_wait_for_node(selector="class:LoginScreen", state="removed")
```

### Visual Regression Testing

```
1. godot_launch(project_path="...", headless=false)
2. godot_wait_for_node(selector="class:MainMenu", state="visible")
3. godot_screenshot_save_baseline(name="main_menu")
4. # ... make code changes, relaunch ...
5. godot_wait_for_node(selector="class:MainMenu", state="visible")
6. godot_screenshot_diff(name="main_menu", threshold=0.01)
```

### Monitor Performance During Gameplay

```
1. godot_get_performance()
2. godot_press_action(action="move_right", hold_ms=5000)
3. godot_assert_performance(monitor="TIME_FPS", threshold=30, op="gte")
```

### Record and Replay a Play Session

```
1. godot_record_start(output_path="res://recordings/menu_flow.json")
2. # ... interact with the game manually or via other tools ...
3. godot_record_stop()
4. # Later, replay:
5. godot_replay(input_path="res://recordings/menu_flow.json")
```

### Wait for Game State Transition

```
1. godot_press_action(action="interact")
2. godot_wait_for_property(selector="name:Door", property="is_open", operator="equals", expected_value=true)
3. godot_screenshot()
```

## Troubleshooting

### "Not connected. Call godot_connect or godot_launch first."
- You must call `godot_launch` or `godot_connect` before any other tool.

### "Connection refused"
- Game not running, or not started with `--stagehand` flag / `STAGEHAND_ENABLED=1` env var.
- Check the port is correct (default 26700).

### "Connection reset" / immediate disconnect
- Godot may be under heavy load (large scene import, startup).
- Increase `timeout_ms` on launch, or retry `godot_connect` after a few seconds.

### Screenshots are black or empty
- Headless mode (`headless: true`) cannot produce meaningful screenshots — use `headless: false`.
- On Windows/WSL, ensure the Godot window is visible and not minimized.

### Timeout on launch
- First launch in a project triggers Godot's asset import phase (30-60s extra).
- Use generous `timeout_ms` values (45000-90000) for cold starts.
- Verify the `project_path` contains a valid `project.godot` file.

### Invalid selector errors
- Check selector syntax: prefixes are `name:`, `class:`, `group:`, `text:`, `meta:`, `unique:`.
- Chain separator is ` >> ` (with spaces).
- Empty selectors or empty values after the prefix are rejected.

### Blocked method errors
- Methods starting with `_` (private/lifecycle) are blocked.
- Destructive methods (`free`, `queue_free`, `set_script`, `add_child`, `remove_child`, etc.) are blocked.
- Use `godot_evaluate` if you truly need to call blocked methods (with caution).

### Port conflicts
- Another Godot instance may already be using port 26700.
- Use a different port: `godot_launch(port=26701)` or kill the stale process.

## Best Practices

- **Use selectors over coordinates.** Selectors are resolution-independent and survive layout changes.
- **Wait before interacting.** Always use `godot_wait_for_node` before clicking UI that may not be ready yet.
- **Prefer `godot_press_action` over `godot_press_key`.** Actions are project-defined and self-documenting; raw keys are fragile.
- **Use `headless: false` for visual tests.** Screenshots require a visible window.
- **Use `headless: true` for structural tests.** Faster startup when you only need `find_nodes`, `get_tree`, `evaluate`.
- **Explore before acting.** Start with `godot_get_game_state` + `godot_get_tree(max_depth=3)` to understand the current scene structure.
- **Use chained selectors for precision.** `class:Panel >> text:OK` is safer than just `text:OK` which may match multiple nodes.
