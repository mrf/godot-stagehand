# Minimal Stagehand Example

This is a minimal Godot project demonstrating how to use the Stagehand addon for external game automation and testing.

## Watch an agent play it (one command)

No project setup, no manual Godot steps — this single command builds the CLI,
launches this exact project headless, and drives it: it waits for the scene,
reads the label, clicks the button, and asserts the label changed. It's the
scenario runner from [docs/cli.md](../../docs/cli.md), pointed at
[`scenarios/watch-agent-play.json`](scenarios/watch-agent-play.json).

From the repository root, with a Godot 4.3+ binary on `PATH` or `$GODOT_BIN`:

```bash
go build -o godot-stagehand .
GODOT_BIN=/path/to/godot ./godot-stagehand run examples/minimal-game/scenarios/watch-agent-play.json
```

Exit code `0` means every step passed — the agent found the scene, read the
label's starting text, clicked the button, and confirmed the label updated to
"Button was pressed!". Add `--out-dir some-dir` to also collect
`report.json`, `junit.xml`, `rpc-trace.json`, and the engine's own log.

This runs headless (no window), so it's the structural half of the loop, not
a visual one — see [Testing Automation](#testing-automation) below for the
interactive tools (screenshots included) once you connect an MCP client
instead.

## Setup Instructions

1. Open this project in Godot Engine (tested with Godot 4.2+)
2. Navigate to `Project → Project Settings → Plugins`  
3. Find the "Stagehand" plugin and enable it
4. The Stagehand addon enables external automation via WebSocket connections

## Usage with Stagehand

To control this game remotely using Stagehand:

### Step 1: Start this Godot project with Stagehand enabled
Run one of these commands in this directory:
```bash
# Option A: Environment variable
STAGEHAND_ENABLED=1 godot --path .

# Option B: Command line argument
godot --path . --stagehand
```

Godot prints a fresh authentication token at startup. Pass it as `auth_token`
when calling `godot_connect`. The listener is loopback-only unless remote access
is explicitly enabled.

If instead the MCP server starts this project for you via `godot_launch`, it
generates a per-launch `STAGEHAND_INSTANCE_TOKEN` and verifies it in the
`ping` response's `instance_token` field — proof that the connection landed
on the process it spawned, not some other Godot instance sharing the port.
There's nothing to configure for this; it's automatic with `godot_launch`.

### Step 2: Start the Stagehand MCP server
From the main godot-stagehand directory:
```bash
go run ./
```

### Step 3: Connect and automate
Using MCP-compatible clients like Claude or custom toolsets, connect to automate interactions with this game.

## Key Elements

This example contains:
- `Main.tscn`: Simple scene with a button and label
- `Main.gd`: Basic interaction code 
- Stagehand addon automatically manages automation connections

## Testing Automation

Try using these Stagehand tools once connected:
- `godot_get_tree` - Get the scene structure
- `godot_find_nodes` with selector `text=Test Button` - exact-text match;
  returns only the Button (the sibling "Test Button toggles the label above"
  Description label does *not* match, since `text=` requires an exact string)
- `godot_find_nodes` with selector `text:Test Button` - substring match;
  returns both the Description label and the Button
- `godot_click` with selector `text:Test Button` - resolves the same
  ambiguous substring match, but ranks the interactive Button above the
  plain Label and clicks it
- `godot_get_property` - Read properties of the label
