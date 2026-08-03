# How Stagehand compares

Back to the [README](../README.md).

### Stagehand drives your *running game*, not the Godot editor.

It has no editor integration, cannot create a scene, and never touches your
project files. It attaches to a game that is **already running and
playing** — clicking real buttons, reading real node state at runtime,
taking real screenshots of real frames.

What sets it apart from other runtime-capable tools: **automated visual
regression** (`screenshot_diff` against saved baselines, not just raw
capture) and a **standalone CI scenario runner** — JUnit output and exit
codes, usable in any pipeline without an MCP client.

Editor-automation tools and Stagehand are complements, not competitors —
plenty of projects will want both.

## Why it exists

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

## Related reading

- [Architecture](architecture.md) — how the addon, the binary, and your client fit together
- [Tool reference](tools.md) — what the MCP frontend actually exposes
- [CLI and scenario runner](cli.md) — the frontend that needs no MCP client
