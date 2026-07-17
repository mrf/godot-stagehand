# Minimal Stagehand Example

This is a minimal Godot project demonstrating how to use the Stagehand addon for external game automation and testing.

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
- `godot_find_nodes` - Find the "Test Button" 
- `godot_click` - Click on the button by selector
- `godot_get_property` - Read properties of the label
