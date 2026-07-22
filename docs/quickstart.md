# Quickstart: Zero to Connected in 5 Minutes

This guide gets you from "never heard of Stagehand" to "Claude is talking to my running game" in under 5 minutes.

**What you need:** Godot 4.3+ installed, Claude Desktop (or another Claude client), and your game project open.

---

## Step 1: What you're installing

Stagehand is two pieces that work together:

- **An addon** that goes inside your Godot project. It listens for commands while your game runs.
- **A small server** that runs on your computer alongside Claude. It passes Claude's requests to the addon.

Once both are in place, Claude can click buttons in your game, read the scene tree, take screenshots, and more — all without you doing it manually.

---

## Step 2: Download the server

Go to the [Releases page](https://github.com/mrf/godot-stagehand/releases) and download the file for your operating system:

| Your OS | File to download |
|---------|-----------------|
| Windows | `godot-stagehand-<version>-windows-amd64.exe` |
| macOS (Apple Silicon) | `godot-stagehand-<version>-darwin-arm64` |
| macOS (Intel) | `godot-stagehand-<version>-darwin-amd64` |
| Linux | `godot-stagehand-<version>-linux-amd64` |

**Where to put it:**

- **Windows:** Move the `.exe` to a folder you'll remember, like `C:\Tools\stagehand\`. Avoid paths with spaces.
- **macOS / Linux:** Move it to `/usr/local/bin/godot-stagehand` for convenience, or any folder in your PATH.

**macOS / Linux: make it executable**

Open a terminal and run:
```bash
chmod +x /path/to/godot-stagehand
```

> If macOS blocks the file with "cannot be opened because the developer cannot be verified": open System Settings → Privacy & Security → scroll down to the blocked app → click **Allow Anyway**.

---

## Step 3: Install the addon

### Option A: Copy the folder manually

1. Download the source zip from the same [Releases page](https://github.com/mrf/godot-stagehand/releases).
2. Unzip it. Inside you'll find an `addons/stagehand/` folder.
3. Copy that entire `addons/stagehand/` folder into **your** project's `addons/` folder.
   - If your project doesn't have an `addons/` folder yet, create one at the top level next to `project.godot`.
4. Your project should now have: `your-project/addons/stagehand/plugin.cfg`

### Option B: Asset Library (when available)

1. In Godot, open your project and click the **AssetLib** tab at the top.
2. Search for **Stagehand**.
3. Click Install, then close the dialog.

![screenshot: Godot editor with AssetLib tab selected, search results showing "Stagehand"](../assets/assetlib-search.png)

### Enable the plugin

After copying the folder, you need to turn the plugin on:

1. In Godot: **Project → Project Settings → Plugins**
2. Find **Stagehand** in the list and check the **Enable** checkbox.

![screenshot: Project Settings Plugins tab with Stagehand listed and Enable checkbox checked](../assets/plugin-enable.png)

If you did it right: the Godot output panel shows `Stagehand: plugin loaded` and a **Stagehand** button appears in the top toolbar.

---

## Step 4: Tell Claude about the server

Claude needs to know where to find the server you downloaded. This is a one-time config change.

**Find your settings file:**

| Claude client | Settings file location |
|--------------|------------------------|
| Claude Desktop (Windows) | `%APPDATA%\Claude\claude_desktop_config.json` |
| Claude Desktop (macOS) | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| Claude Code (Claude CLI) | `.claude/settings.json` inside your project, or `~/.claude/settings.json` globally |

Open that file in any text editor. Add the `mcpServers` section (or add to it if it already exists):

```json
{
  "mcpServers": {
    "godot-stagehand": {
      "command": "/path/to/godot-stagehand"
    }
  }
}
```

Replace `/path/to/godot-stagehand` with the actual path to the file you downloaded:
- **Windows example:** `C:\\Tools\\stagehand\\godot-stagehand-0.2.0-windows-amd64.exe`
- **macOS/Linux example:** `/usr/local/bin/godot-stagehand`

> **Windows path tip:** Use double backslashes (`\\`) or forward slashes (`/`) in JSON — single backslashes will cause a parse error.

After saving, **restart Claude Desktop** (or reload your Claude Code window). You should see `godot-stagehand` listed in Claude's tools — it will appear when you start a new conversation.

---

## Step 5: Run your game with Stagehand enabled

Stagehand is **off by default** — you turn it on when you want it.

### Option A: From the Godot editor

Click the **Stagehand** toggle button in the top toolbar, then press **Play (F5)** as normal.

The toggle is editor-only: it adds `--stagehand` to editor play sessions and is
not exported with your game.

![screenshot: Godot editor toolbar with Stagehand toggle button highlighted](../assets/toolbar-toggle.png)

### Option B: From the command line

```bash
# macOS / Linux
godot --path /path/to/your/project --stagehand

# Windows
"C:\path\to\Godot_v4.x-stable_win64.exe" --path "C:\path\to\your\project" --stagehand
```

**You're good if:** The Godot output panel (or terminal) shows:
```
Stagehand: Authentication token: <one-session-token>
Stagehand: Server listening on port 26700 (127.0.0.1)
```

Keep that token private. You will give it to Claude in the next step; a new
token is generated each time unless you explicitly set `STAGEHAND_AUTH_TOKEN`.

**Try this if not:**
- Make sure the Stagehand plugin is enabled (Step 3).
- Check that your scene has at least one node that processes frames (`_process` or `_physics_process`). An empty or static scene won't tick.

---

## Step 6: Try it — your first command

With your game running, go to Claude and ask it:

> "Connect to my Godot game using auth token `<one-session-token>` and show me the scene tree."

Claude will pass the token to `godot_connect`, then call `godot_get_tree` to read
the scene structure. `godot_launch` handles its fresh token automatically.

**What you should see:**

Claude's response will include something like:
```
Connected to Godot 4.x at 127.0.0.1:26700
Scene tree:
└── Node (root)
    └── Main
        ├── Player
        │   ├── Sprite2D
        │   └── CollisionShape2D
        └── UI
            └── HUD
```

**If you see "Connection refused":** The game isn't running with Stagehand enabled, or it hasn't finished loading yet. Check the output panel for `Stagehand: Server listening`.

**If you see "Authentication required" or "Authentication failed":** Use the
token printed by the currently running Godot session, not one from an earlier run.

**If you're on Windows with Claude running in WSL:** See [Windows / WSL Setup](windows-setup.md) for networking instructions — you'll need one extra step to bridge the network stacks.

---

## Troubleshooting

### "Connection refused" or "failed to connect"

1. Is Godot running? The game window must be open and the scene loaded.
2. Is Stagehand enabled? Look for `Server listening on port 26700` in Godot's output.
3. Are you on Windows with Claude in WSL? See [Windows / WSL Setup](windows-setup.md).
4. Port conflict? Run another instance on a different port:
   - Start Godot with `--stagehand-port=26701`
   - In Claude: "Connect to my game on port 26701"

### "Plugin not found" or Stagehand doesn't appear in Project Settings

1. Confirm `addons/stagehand/plugin.cfg` exists in your project folder.
2. In Godot: **Project → Project Settings → Plugins** — if Stagehand isn't listed, the folder is in the wrong place.
3. Try: close Project Settings, wait a moment, reopen it.

### Screenshots come back black or empty

Stagehand can only capture what Godot actually renders. Two common causes:

- **Headless mode:** If you launched with `--headless`, there's nothing to render. Launch normally (with a visible window).
- **Window minimized:** Restore the window and try again.

---

## What's next

- **More tools:** See the [full tool reference](../README.md#available-tools) — you can click buttons, set properties, wait for signals, record input, and more.
- **Selectors:** Learn how to target specific nodes by name, class, group, or text — see [Selectors Guide](../SELECTORS_GUIDE.md).
- **Windows / WSL details:** [Windows / WSL Setup](windows-setup.md).
