# Godot Stagehand

External automation and testing for running Godot games — like Playwright, but for game engines.

An MCP server (Go) + Godot addon (GDScript) that lets AI agents, test runners, and CI pipelines connect to a running Godot game and interact with it programmatically: navigate scenes, click buttons, read node properties, take screenshots, wait for conditions.

## The Problem

There's no way to control a running Godot game from the outside. Existing tools either test code in-process (GdUnit4, GUT) or control the editor (godot-mcp servers). Nothing bridges an external process to a live game the way browser automation tools do for the web.

## How It Works

```
Claude / AI Agent  <──  MCP (JSON-RPC over stdio)  ──>  godot-stagehand (Go)
                                                              │
                                                         WebSocket
                                                       JSON-RPC 2.0
                                                              │
                                                        Running Game
                                                     with stagehand addon
```

Three layers:

1. **MCP Client** — Claude, any AI agent, or a test harness sends tool calls
2. **Go MCP Server** — translates MCP tools into WebSocket messages
3. **Godot Addon** — GDScript WebSocket server inside the game, executes commands against the scene tree

## What It Will Do

- **Query the scene tree** — find nodes by path, name, class, or group
- **Read and write properties** — inspect and modify any node property
- **Simulate input** — click, type, press keys, trigger input actions
- **Take screenshots** — capture the viewport as PNG
- **Wait for conditions** — poll until a node exists, a property changes, or a signal fires
- **Launch and connect** — start Godot or attach to an already-running game
- **Evaluate GDScript** — run arbitrary expressions in the game context

## Current Status

**Early development.** Project structure and design are in place, implementation is underway. Nothing is usable yet.

See [DESIGN.md](DESIGN.md) for the full architecture, protocol spec, and API surface.

## Roadmap

**MVP** — GDScript WebSocket server, core MCP tools (connect, get_tree, find_nodes, get/set property, screenshot, input simulation), basic selectors (path, name, class, group)

**Phase 2** — Auto-launch Godot, wait-for conditions, method calls, GDScript evaluation, scene changes, advanced selectors (text, metadata, chaining)

**Phase 3** — Signal waiting, accessibility tree integration, visual regression testing, record-and-replay, CI/CD integration, multi-instance support

## Requirements

- Godot 4.x
- Go 1.21+

## License

TBD
