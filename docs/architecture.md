# Architecture

Back to the [README](../README.md).

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

## Deeper dives

- [MCP vs CLI vs both](architecture/mcp-vs-cli.md) — why there are two frontends and not one built on the other
- [Instance isolation](architecture/instance-isolation.md) — how two instances of the same project avoid sharing `user://` and the `.godot` import cache
- [Error model](error-model.md) — failure kinds and their JSON-RPC codes
- [Addon sync contract](addon-sync-contract.md) — which copy of the addon is authoritative
