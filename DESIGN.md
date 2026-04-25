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
| `ping` | Health check, returns engine info |
| `get_tree` | Full scene tree snapshot |
| `query_nodes` | Find nodes matching a selector |
| `get_property` | Read a property from a node |
| `set_property` | Write a property on a node |
| `call_method` | Call a method on a node |
| `change_scene` | Load a new scene |
| `screenshot` | Capture viewport as PNG base64 |
| `input_action` | Simulate an input action press/release |
| `input_mouse` | Simulate mouse click/move at coordinates |
| `input_key` | Simulate keyboard key press/release |
| `input_touch` | Simulate touch event |
| `wait_condition` | Poll until a condition is met |
| `wait_signal` | Wait for a signal to be emitted |
| `evaluate` | Execute arbitrary GDScript expression |
| `get_game_state` | Current scene, FPS, physics state, window size |

## Selector System

Selectors are strings with a prefix-based grammar, inspired by Playwright's locators.

| Prefix | Example | Matches |
|--------|---------|---------|
| *(none)* | `"/root/UI/StartButton"` | Exact node path |
| `name:` | `"name:*Button*"` | Node name with glob matching |
| `class:` | `"class:Button"` | All nodes of a given Godot class |
| `group:` | `"group:interactive"` | All nodes in a group |
| `text:` | `"text:Start Game"` | Control nodes with matching `text` property |
| `meta:` | `"meta:pw_id=start_btn"` | Nodes with matching metadata |
| `unique:` | `"unique:StartButton"` | Unique name reference (`%StartButton`) |

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
| `text:` | Tree walk + check `text` property on Controls |
| `meta:` | Tree walk + `has_meta()` / `get_meta()` |
| `unique:` | `get_node("%" + name)` |

## MCP Tool Set

### Navigation & Scene Management

**`godot_change_scene`**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `scene_path` | string | yes | Resource path, e.g. `"res://scenes/main_menu.tscn"` |

Returns: `{ success, current_scene }`

**`godot_get_game_state`**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| *(none)* | | | |

Returns: `{ current_scene, fps, physics_ticks, window_size, connected, engine_version }`

### Node Querying

**`godot_get_tree`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `root_path` | string | no | `"/root"` | Subtree root |
| `max_depth` | int | no | `10` | Recursion depth |
| `include_properties` | string[] | no | `[]` | Properties to include per node |

Returns: Recursive `{ name, class, path, children, properties }` tree.

**`godot_find_nodes`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `selector` | string | yes | | Selector expression |
| `properties` | string[] | no | `[]` | Properties to return per match |
| `limit` | int | no | `50` | Max results |

Returns: `{ nodes: NodeInfo[], count }`

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

Returns: `{ result }`

**`godot_evaluate`**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `expression` | string | yes | GDScript expression |
| `context_node` | string | no | Node path for `self` context |

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

**`godot_press_action`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `action` | string | yes | | Input action name (`"ui_accept"`, `"move_left"`) |
| `strength` | float | no | `1.0` | |
| `hold_ms` | int | no | `100` | |

Returns: `{ success }`

**`godot_mouse_move`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `position` | `{x, y}` | yes | | Target position |
| `steps` | int | no | `10` | Interpolation steps |

Returns: `{ success }`

### Visual Inspection

**`godot_screenshot`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `selector` | string | no | | Crop to this node's rect |
| `full_page` | bool | no | `true` | |

Returns: MCP `ImageContent` (base64 PNG)

### Waiting

**`godot_wait_for_node`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `selector` | string | yes | | Node to wait for |
| `state` | string | no | `"exists"` | `"exists"`, `"visible"`, `"removed"` |
| `timeout_ms` | int | no | `5000` | |

Returns: `{ found, elapsed_ms, node }`

**`godot_wait_for_signal`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `selector` | string | yes | | Node that emits the signal |
| `signal_name` | string | yes | | Signal to wait for |
| `timeout_ms` | int | no | `5000` | |

Returns: `{ received, elapsed_ms, args }`

**`godot_wait_for_property`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `selector` | string | yes | | Target node |
| `property` | string | yes | | Property to check |
| `value` | any | yes | | Expected value |
| `comparator` | string | no | `"eq"` | `"eq"`, `"neq"`, `"gt"`, `"lt"`, `"contains"` |
| `timeout_ms` | int | no | `5000` | |

Returns: `{ matched, elapsed_ms, actual_value }`

### Lifecycle

**`godot_connect`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `host` | string | no | `"localhost"` | |
| `port` | int | no | `26700` | |

Returns: `{ connected, engine_version, game_title }`

**`godot_launch`**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `project_path` | string | yes | | Path to Godot project directory |
| `scene` | string | no | | Specific scene to run |
| `godot_path` | string | no | | Path to Godot binary |
| `headless` | bool | no | `false` | |
| `extra_args` | string[] | no | `[]` | |

Returns: `{ pid, port, connected }`

## GDScript Addon Structure

```
addons/stagehand/
  plugin.cfg                    # Editor plugin metadata
  plugin.gd                     # Editor plugin (toolbar button to start/stop server)
  autoload/
    stagehand_server.gd         # WebSocket server — the core autoload
  core/
    command_router.gd           # Routes JSON-RPC methods to handlers
    selector_engine.gd          # Parses and resolves selector expressions
    tree_serializer.gd          # Serializes scene tree to JSON-safe dicts
    screenshot_capture.gd       # Viewport capture + base64 encoding
    input_simulator.gd          # Synthesizes InputEvent objects
    waiter.gd                   # Wait-for-condition polling + signal waiting
    expression_evaluator.gd     # Evaluates GDScript expressions
  protocol/
    json_rpc.gd                 # JSON-RPC 2.0 message parsing/construction
```

### Activation Guard

The WebSocket server only starts when explicitly enabled:

- Environment variable `STAGEHAND_ENABLED=1`, or
- Command-line flag `--stagehand`

The autoload checks on `_ready()` and disables itself otherwise. This prevents the server from running in production builds.

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
  main.go                        # Entry point, CLI flags
  internal/
    mcpserver/
      server.go                  # MCP server setup, tool registration
      tools_navigation.go        # godot_change_scene, godot_get_game_state
      tools_query.go             # godot_get_tree, godot_find_nodes
      tools_property.go          # godot_get_property, godot_set_property
      tools_method.go            # godot_call_method, godot_evaluate
      tools_input.go             # godot_click, godot_press_key, godot_type_text, etc.
      tools_visual.go            # godot_screenshot
      tools_wait.go              # godot_wait_for_*
      tools_lifecycle.go         # godot_connect, godot_launch
    godotconn/
      conn.go                    # WebSocket connection to Godot addon
      reconnect.go               # Reconnection with exponential backoff
      protocol.go                # GWP message types, JSON-RPC helpers
      launcher.go                # Launch Godot process, wait for addon ready
    selector/
      parse.go                   # Selector parsing and validation
      parse_test.go
  testdata/
    test_project/                # Minimal Godot project for integration tests
      project.godot
      addons/stagehand/          # Symlink to the addon
      scenes/
        test_scene.tscn
        test_ui.tscn
  addons/
    stagehand/                   # The GDScript addon (distributed from here)
```

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

### Startup Flow

1. MCP server starts, listens on stdio
2. Agent calls `godot_connect` (attach to running game) or `godot_launch` (start Godot)
3. If launching: Go server starts Godot with `--stagehand` flag, polls WebSocket until ready
4. Go server sends `ping`, gets engine info
5. All subsequent tool calls use this connection

### Error Handling

- Tools called before connection return `isError: true` with message: *"Not connected. Call godot_connect or godot_launch first."*
- During reconnection, tool calls queue for up to 3 seconds, then fail
- On reconnect, pending waits are cancelled

### Multiple Instances

- Each Go MCP server process connects to one Godot instance
- Different ports for parallel testing
- `godot_launch` returns the assigned port

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
    proc, _ := launcher.Launch(ctx, LaunchOptions{
        ProjectPath: "testdata/test_project",
        Headless:    true,
    })
    defer proc.Kill()
    conn, _ := godotconn.Dial(ctx, "localhost", proc.Port)
    resp, _ := conn.Call(ctx, "get_tree", map[string]any{"max_depth": 3})
    assert.Equal(t, "root", resp.Result.Tree.Name)
}
```

### End-to-End MCP Tests

In-memory MCP transports with a mock Godot WebSocket server to test the full Claude → MCP → GWP → response flow.

## Phased Delivery

### MVP (Phase 1)

- GDScript addon: WebSocket server, `ping`, `get_tree`, `query_nodes`, `get_property`, `set_property`, `screenshot`, `input_action`, `input_mouse`, `input_key`
- Go MCP server: `godot_connect`, `godot_get_tree`, `godot_find_nodes`, `godot_get_property`, `godot_set_property`, `godot_screenshot`, `godot_click`, `godot_press_key`, `godot_press_action`, `godot_get_game_state`
- Selectors: path, name, class, group
- Connection: manual connect only
- Tests: unit tests for selectors, one integration test

### Phase 2

- `godot_launch` (auto-start Godot)
- `godot_wait_for_node`, `godot_wait_for_property`
- `godot_call_method`, `godot_evaluate`
- `godot_change_scene`
- `text:` and `meta:` selectors, `>>` chaining
- `godot_type_text`, `godot_mouse_move`

### Phase 3

- `godot_wait_for_signal`
- Accessibility tree integration (AccessKit on 4.5+)
- Visual regression testing (screenshot diffing)
- Record-and-replay mode
- Performance profiling tools
- CI/CD integration (GitHub Actions)
- Multi-instance testing (networked games)

## Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| **WebSocket over TCP** | Provides framing (no length-prefix needed), well-supported in Go (`gorilla/websocket`) and GDScript (`WebSocketPeer`), works through proxies |
| **JSON-RPC 2.0** | Same protocol MCP uses — consistent, `id`-based correlation for multiplexing |
| **Polling for waits** | Simpler than signal-based. At 60fps, 16ms polling is fast enough for automation |
| **Go for MCP server** | User's preferred backend language, strong concurrency primitives, single binary distribution |
| **Port 26700** | Distinct from Godot's debugger (6007). Configurable via flag/env var |
| **Autoload over editor-plugin-only** | Addon must run inside the *game* process, not just the editor. Autoload is the simplest mechanism |
| **Env var activation guard** | `STAGEHAND_ENABLED=1` or `--stagehand` flag. WebSocket server must never run in production builds |
| **Selector prefix grammar** | Inspired by Playwright's locator strategies but adapted for Godot's node tree (paths, groups, classes instead of CSS/ARIA) |
