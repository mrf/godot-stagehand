# Architecture: MCP vs CLI vs Both

**Status:** Decided — keep MCP as primary, architect for future CLI  
**Date:** 2026-05-16

## Problem

Godot Stagehand exposes game automation exclusively as an MCP server (stdio JSON-RPC). This works well for AI agents but is invisible to humans — you can't script it in CI, debug a single call from the terminal, or use it without an MCP client.

Should MCP remain the sole interface, or do we need a CLI alongside it?

## Decision

**Keep MCP as the primary interface. The architecture already supports adding a CLI frontend later with no refactoring required.** No changes needed now.

## Current Architecture

```
Claude / MCP Client  <-->  stdio (MCP protocol)  <-->  Go binary  <-->  WebSocket  <-->  Godot addon
```

Internal package structure:

```
main.go
  └── mcpserver.New() → mcpserver.Serve()   (MCP-specific: stdio, tool registration)
        ├── handleClick(req) → callGodot("input_mouse", params)
        └── ... (23 handlers, all same pattern)

internal/godotconn/    ← Protocol-agnostic. WebSocket + JSON-RPC 2.0. No MCP imports.
internal/selector/     ← Protocol-agnostic. Pure parsing. No MCP imports.
internal/launch/       ← Protocol-agnostic. Process management. No MCP imports.
```

## Why MCP Stays Primary

- **AI-native tool calling is the killer feature.** Typed schemas, safety annotations, stateful sessions. Shelling out to a CLI loses all of this.
- **Ecosystem compatibility.** Any MCP client (Claude, Cursor, Windsurf, custom) gets zero-config integration.
- **Composability.** AI naturally chains launch → screenshot → click → wait → assert.

## Why CLI Makes Sense Eventually

- **CI/CD.** GitHub Actions shouldn't need an MCP client to run `godot-stagehand screenshot --save baseline.png`.
- **Debugging.** `godot-stagehand tree --depth 2` is faster than firing up an MCP inspector.
- **Adoption funnel.** Humans try the CLI → understand the tool → configure MCP for Claude.

## Future CLI Design

```
main.go
  ├── "serve" subcommand → mcpserver (current behavior, default)
  └── "click", "tree", "screenshot", ... → CLI frontend
        ├── Parse flags/args → map[string]any
        ├── Validate (reuse selector.ParseChain)
        ├── godotconn.Dial(addr) → conn
        └── conn.Call(ctx, method, params) → print result
```

- **Connection model:** One-shot (connect per call, ~50-100ms). No daemon needed.
- **Same binary, subcommands.** `godot-stagehand serve` (MCP) vs `godot-stagehand click ...` (CLI).
- **Validation reuse:** Selector parsing and blocked-method enforcement already live in protocol-agnostic packages.

## Why No Changes Are Needed Now

| Concern | Assessment |
|---------|-----------|
| Internal packages leaking MCP types? | No. `godotconn`, `selector`, `launch` have zero MCP imports. |
| Business logic trapped in handlers? | Handlers are just param extraction + `callGodot()`. Logic is in Godot. |
| Need to refactor Server to share code? | No. CLI bypasses Server — talks to godotconn directly. |
| GWP protocol assumes MCP? | No. Pure JSON-RPC 2.0. Any client can speak it. |

## When to Revisit

Build the CLI when one of these becomes a real workflow (not hypothetical):
- CI pipeline that needs to run Stagehand commands without an MCP client
- Manual QA workflow where a developer pokes at a running game from terminal
- Third-party tooling that wants to integrate without MCP
