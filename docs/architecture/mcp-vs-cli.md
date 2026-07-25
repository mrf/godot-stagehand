# Architecture: MCP vs CLI vs Both

**Status:** Superseded — the CLI was built (2026-07-24). MCP remains primary.
**Original decision:** 2026-05-16 — keep MCP as primary, architect for a future CLI
**Revisited:** 2026-07-24 — the CI trigger fired; CLI and scenario runner shipped

## Problem

Godot Stagehand exposed game automation exclusively as an MCP server (stdio
JSON-RPC). This worked well for AI agents but was invisible to humans — you
could not script it in CI, debug a single call from the terminal, or use it
without an MCP client.

## Decision (2026-05-16)

Keep MCP as the primary interface. The architecture already supports adding a
CLI frontend later with no refactoring required.

## Revision (2026-07-24)

The documented trigger — *"a CI pipeline that needs to run Stagehand commands
without an MCP client"* — became a real requirement, and the README was already
claiming CI usability the tool could not deliver. The CLI was built. MCP stays
primary and its tool surface is unchanged.

The 2026-05-16 prediction held: no MCP-facing refactoring was needed. What the
build did require was extracting the *shared, non-MCP* logic that had
accumulated inside `mcpserver` — see "What actually had to move" below.

## Architecture as built

```
                       ┌──────────────────────────────────────┐
  MCP client ──stdio──►│ mcpserver     │  cli                 │
                       │  (tool        │   ├── one-shot cmds  │◄── argv
  CI / terminal ──────►│   schemas)    │   └── scenario       │
                       └───────┬───────┴──────────┬───────────┘
                               │                  │
                    ┌──────────▼──────────────────▼──────────┐
                    │  gwpop   selector   visual   launch    │
                    │      (protocol-agnostic core)          │
                    └───────────────────┬────────────────────┘
                                        │
                                  godotconn ──WS──► Godot addon
```

| Package | Role |
|---------|------|
| `internal/godotconn` | WebSocket + JSON-RPC 2.0 transport. No MCP, no CLI |
| `internal/selector` | Selector parsing |
| `internal/launch` | Process management |
| `internal/gwp` | Protocol version, capabilities, handshake, error rendering |
| `internal/gwpop` | Action registry: action name → GWP method + validated params; typed failure kinds; the blocked-method list; `Connect`; `Capture` |
| `internal/visual` | Screenshot decode, baseline save, pixel diff, artifact writing |
| `internal/mcpserver` | MCP tool schemas and handlers |
| `internal/cli` | One-shot commands, flag parsing, exit codes |
| `internal/scenario` | Scenario model, validation, runner, JSON/JUnit/trace reporters |

`cli` and `mcpserver` are siblings. Neither imports the other.

## Why MCP stays primary

- **AI-native tool calling is the killer feature.** Typed schemas, safety
  annotations, stateful sessions. Shelling out to a CLI loses all of this.
- **Ecosystem compatibility.** Any MCP client gets zero-config integration.
- **Composability.** AI naturally chains launch → screenshot → click → wait → assert.

## Why the CLI earns its place

- **CI/CD.** A pipeline runs `godot-stagehand run scenario.json` and branches
  on the exit code. No MCP client, no wrapper script.
- **Debugging.** `godot-stagehand tree --port N --max-depth 2` is faster than
  firing up an MCP inspector.
- **Adoption funnel.** Humans try the CLI → understand the tool → configure MCP.

## What actually had to move

The 2026-05-16 note claimed handlers were "just param extraction +
`callGodot()`". That was true for most of them, but three pieces of real,
non-MCP logic had settled inside `mcpserver` and would otherwise have been
duplicated:

1. **The screenshot/baseline/diff pipeline** — base64 decode, PNG validation,
   dimension cross-checking, baseline files, diff artifacts, the outcome
   records. Extracted to `internal/visual`; `mcpserver`'s structured outcome
   types are now aliases of it, so the MCP and CLI surfaces cannot report the
   same comparison with different fields.
2. **The blocked-method list** — defense-in-depth against destructive remote
   calls. Moved to `internal/gwpop`; two copies could have drifted into a real
   security gap.
3. **Addon error rendering** — the `{error, error_code, details}` triple the
   addon embeds in an otherwise successful JSON-RPC result. Moved to
   `gwp.FormatError`.

`mcpserver` was deliberately *not* rerouted through `gwpop`. Its tool schemas
are its contract, mcp-go already validates against them, and rewriting 30
handlers to share parameter-name literals would have been risk without benefit.
The parameter names appear in two places; the behaviour does not.

## Deliberate constraints

- **Scenario files are JSON, not YAML.** The module has two direct
  dependencies and no YAML parser; adding one for authoring sugar was not
  worth it. Revisit if scenario authoring becomes a common complaint.
- **The CLI requires an explicit `--port`.** The addon's shared default 26700
  routinely belongs to another agent's game. Silently driving someone else's
  SceneTree is worse than a usage error. Same rule for connect-mode scenarios.
- **One-shot commands connect; they do not launch.** Launching belongs to the
  scenario runner, which owns the process lifetime and kills it afterwards.
  A one-shot that launched a game would have to decide when to kill it.
- **Scenario artifact paths cannot escape the run's directories.** Scenario
  files are data and may arrive in a pull request.
- **Flags may follow positional arguments.** Go's `flag` package stops at the
  first non-flag token, which would silently ignore `find sel --limit 5`. The
  CLI permutes arguments before parsing; a positional starting with `-` needs
  the conventional `--` terminator.

## Compatibility contract

A **no-argument invocation is the MCP stdio server** and always will be. Every
configured MCP client launches the binary that way and speaks JSON-RPC over
stdin/stdout immediately; anything else printed there corrupts the transport.
`TestNoArgumentInvocationStaysAnMCPStdioServer` drives the built binary through
a real `initialize` handshake to pin it. `serve` is an explicit alias for the
same behaviour.

## Further reading

- [CLI and scenario runner guide](../cli.md)
- `.github/workflows/ci.yml` → the `scenario-smoke` job
