# MCP tool reference

Every tool the MCP frontend exposes. Back to the [README](../README.md); for the
non-MCP frontend see the [CLI and scenario runner guide](cli.md).

Point an MCP client — Claude Code, Claude Desktop, Cursor, anything that speaks
MCP — at the binary and it gets a live connection to your running game:

```json
{
  "mcpServers": {
    "godot-stagehand": {
      "command": "/absolute/path/to/godot-stagehand"
    }
  }
}
```

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
| `godot_screenshot_save_baseline` / `godot_screenshot_diff` | Visual regression testing ([guide](visual-regression.md)) |
| `godot_wait_for_node` | Wait for node to exist |
| `godot_wait_for_signal` | Wait for signal emission |
| `godot_wait_for_property` | Wait for property condition |
| `godot_change_scene` | Change scenes |
| `godot_get_game_state` | Runtime info (scene, FPS, window) |
| `godot_get_performance` / `godot_assert_performance` | Performance monitoring |
| `godot_record_start` / `godot_record_stop` / `godot_replay` | Input recording/replay |

Nodes are targeted with selectors — see the [selector guide](selectors.md).

## What you can build with them

- **AI-assisted playtesting.** Let Claude explore your game, find bugs, and verify fixes without manual clicking.
- **Visual regression testing.** Save baseline screenshots, diff them later. Catch UI regressions before your players do. See the [visual smoke contract](visual-smoke-contract.md) for how to set up a visual gate in your game repo. **Headless Godot cannot render real screenshots**, so this needs a visible window (a real display or something like Xvfb) even in CI.
- **Input recording/replay.** Record a play session's input events with millisecond timestamps, then replay them on the same wall-clock schedule, optionally sped up. This reproduces a rough repro case, not a frame-perfect deterministic run: actual game state during replay still depends on frame timing, which can vary between runs. The on-disk format is versioned; see the [recording format](recording-format.md).
- **Performance monitoring.** `godot_assert_performance` can sample and assert monitors: an optional warm-up, a fixed sample count or duration, and a statistic (min, max, mean, median, p95) to threshold against, instead of one instantaneous read. This is still not proven statistical regression gating (no baseline tracking, outlier rejection, or variance-aware thresholds), so treat it as a steadier smoke check, not a certified regression gate.
- **Agent skill.** [`skills/stagehand.md`](../skills/stagehand.md) teaches an agent the full tool workflow — launch, inspect, interact, test — so you don't have to re-explain it in every session. Point your agent at the file directly, or copy it into wherever your client loads custom skills from (e.g. a Claude Code project's `.claude/skills/` directory). It ships in this source tree, not in the binary release, since it's a prompt file rather than a runtime asset.

Running more than one agent against one game needs care — see
[Configuration → running several agents at once](configuration.md#running-several-agents-at-once).
