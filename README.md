# Godot Stagehand

External automation and testing for running Godot games — like Playwright, but for game engines.

An MCP server (Go) + Godot addon (GDScript) that lets AI agents, test runners, and CI pipelines connect to a running Godot game and interact with it programmatically: navigate scenes, click buttons, read node properties, take screenshots, wait for conditions.

## Current Status

**Beta Ready** — Core automation features implemented, documented, and functional. This is a working implementation of Phase 1 (MVP) features as outlined in [DESIGN.md](DESIGN.md).

### ✅ Currently Working Features (Phase 1)
- **`godot_connect`** — Connect to a running Godot game with the stagehand addon
- **`godot_get_tree`** — Get a snapshot of the scene tree with optional property inclusion
- **`godot_find_nodes`** — Find nodes by path, name, class, or group selector
- **`godot_get_property`** and **`godot_set_property`** — Read/write node properties
- **`godot_click`** — Click on nodes or screen positions
- **`godot_press_key`** — Simulate keyboard input
- **`godot_press_action`** — Trigger Godot input actions
- **`godot_screenshot`** — Capture game viewport as image
- **`godot_get_game_state`** — Get basic game runtime info (scene, FPS, etc.)

### 🔜 Missing Features (Phase 2/3)
*These are planned for future releases and are explicitly NOT yet implemented:*
- **`godot_launch`** — Auto-start Godot from MCP (manual launch required for now)
- **`godot_wait_for_*`** functions — Wait for conditions/signals (currently in development)
- **`godot_call_method`**, **`godot_evaluate`** — Execute GDScript code
- **`godot_change_scene`** — Change scenes programmatically  
- Advanced selectors (`text:`, `meta:`, `>>` chaining)

## Quickstart Guide

Follow these steps to get started with Godot Stagehand:

### Prerequisites
- **Godot 4.x** installed (developed/tested with 4.2+)
- **Go 1.21+** installed for building the MCP server
- Your Godot project should have some user interface elements or nodes for interaction

### Step 1: Build the MCP Server
```bash
# Clone or navigate to the godot-stagehand directory
cd godot-stagehand

# Build the binary
go build -o godot-stagehand .

# Or install globally
go install .
```

### Step 2: Install Addon in Your Godot Project
1. Copy the `addons/stagehand/` directory to your Godot project's `addons/` directory
2. In Godot editor, go to Project > Project Settings > Plugins tab
3. Find "Stagehand" in the list and change its status from "Inactive" to "Active"

**Alternative manual installation**: Copy the entire `addons/` folder from this repository to your Godot project root.

### Step 3: Run Godot with Stagehand Enabled
You must enable the websocket server by running Godot with either:

Option A: Environment variable:
```bash
STAGEHAND_ENABLED=1 godot --path /path/to/your/project
```

Option B: Command line flag:
```bash
godot --path /path/to/your/project --stagehand
```

The addon will show a "Stagehand" toggle in the Godot editor toolbar that enables the server when the project is run.

### Step 4: Start the Stagehand MCP Server
In a separate terminal:
```bash
./godot-stagehand
```

This will start the MCP server which AI tools like Claude Code can connect to.

### Step 5: Configure MCP Client
Configure your MCP client (like Claude Code) to recognize the Stagehand tools by pointing your AI assistant to the server.

The following example MCP configuration shows how to enable Godot Stagehand tools:

#### MCP Client Configuration Example
```json
{
  "mcpServers": {
    "godot-stagehand": {
      "command": [
        "/absolute/path/to/godot-stagehand"
      ],
      "env": {
        "PATH": "/absolute/path/to/godot-stagehand/binary/directory:$PATH"
      }
    }
  }
}
```

### Step 6: Use Automation Tools After Launch
Once connected, you can use these basic automation tools:

1. **Connect to the game**:
   ```json
   {
     "toolName": "godot_connect",
     "arguments": {
       "host": "localhost",
       "port": 26700
     }
   }
   ```

2. **Get the scene tree** to explore the game structure:
   ```json
   {
     "toolName": "godot_get_tree",
     "arguments": {
       "max_depth": 5
     }
   }
   ```

3. **Find and interact with UI elements**:
   ```json
   {
     "toolName": "godot_find_nodes",
     "arguments": {
       "selector": "class:Button",
       "properties": ["text", "visible"]
     }
   }
   ```

4. **Click a button** by selector or path:
   ```json
   {
     "toolName": "godot_click",
     "arguments": {
       "selector": "class:Button",
       "button": "left"
     }
   }
   ```

5. **Simulate keyboard input**:
   ```json
   {
     "toolName": "godot_press_key",
     "arguments": {
       "key": "Space",
       "modifiers": []
     }
   }
   ```

6. **Capture a screenshot**:
   ```json
   {
     "toolName": "godot_screenshot",
     "arguments": {
       "full_page": true
     }
   }
   ```

## Available Selectors

Currently supported selectors (MVP implementation):

| Syntax | Example | Finds |
|--------|---------|-------|
| Path (exact) | `"/root/UI/StartButton"` | Node at exact scene path |
| Name pattern | `"name:*Button*"` | All nodes whose name contains "*Button*" |
| Class | `"class:Button"` | All Button nodes |
| Group | `"group:interactive"` | All nodes in the "interactive" group |

## Troubleshooting

### Common Issues and Solutions:

1. **Connection fails immediately after connecting:**
   - Ensure the Godot project has the stagehand addon installed and enabled
   - Verify the addon is enabled in the Godot editor plugins panel
   - Make sure you launched Godot with `STAGEHAND_ENABLED=1` or `--stagehand` flag
   - Check that no other apps are using port 26700 (default)

2. **Addon not detected or "not enabled" error:**
   - Verify the `addons/stagehand/` folder structure exists in your Godot project
   - Check that the addon is activated in Project Settings > Plugins
   - Ensure you're running Godot with the environment variable or flag to enable it

3. **Headless Godot doesn't work as expected:**
   - Godot Stagehand works best with visible GUI elements in headed mode
   - Some input simulations may behave unexpectedly in headless mode
   - Recommended: primarily test with GUI-enabled Godot sessions

4. **Port conflict with multiple instances:**
   - By default, Godot Stagehand uses port 26700
   - Use environment variable `STAGEHAND_PORT=XXXX` to specify different port
   - Or use command line: `godot --stagehand --stagehand-port=XXXX`

5. **No response when using automation commands:**
   - Ensure scene is loaded before attempting automation
   - Verify target nodes exist before referencing them
   - Check Godot console for errors (the addon prints server status messages)

## Development and Contributing

This project is under active development with phases clearly delineated in the [DESIGN.md](DESIGN.md).

### Project Structure:
- **`internal/mcpserver/`** — MCP tools and server management (Go)
- **`addons/stagehand/`** — Godot addon with WebSocket server (GDScript)
- **`internal/godotconn/`** — Godot Websocket protocols and connection handling (Go)
- **`internal/selector/`** — Selector parsing logic shared between languages (Go)

## License

MIT — same as Godot itself. See [LICENSE](LICENSE).

## Acknowledgments

- Inspired by [Playwright](https://playwright.dev/) for web automation
- Built on the [MCP Protocol](https://github.com/modelcontextprotocol/specification) specification
- Uses WebSockets for reliable cross-language communication between Go and Godot