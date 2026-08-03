# Quickstart: Connect Claude to Your Game

This guide gets you from "never heard of Stagehand" to "Claude is talking to my running game."

**What you need:** Godot 4.3+ installed, a Claude client (Claude Desktop or Claude Code), and your game project open.

---

## Step 1: What you're installing

Stagehand is two pieces that work together:

- **An addon** that goes inside your Godot project. It listens for commands while your game runs.
- **A small server program** that runs on your computer alongside Claude. It passes Claude's requests to the addon.

Once both are in place, Claude can click buttons in your game, read the scene tree, take screenshots, and more — all without you doing it manually.

---

## Step 2: Install the addon

The addon is plain GDScript, so you don't need the server program yet to complete this step.

1. Go to the [repository page](https://github.com/mrf/godot-stagehand) and download the source: **Code → Download ZIP**.
2. Unzip it. Inside you'll find an `addons/stagehand/` folder.
3. Copy that entire `addons/stagehand/` folder into **your** project's `addons/` folder.
   - If your project doesn't have an `addons/` folder yet, create one at the top level next to `project.godot`.
4. Your project should now have: `your-project/addons/stagehand/plugin.cfg`
5. In Godot: **Project → Project Settings → Plugins**, find **Stagehand** in the list, and check the **Enable** checkbox.

**You're good if:** a **Stagehand** toggle button and a **Setup…** button appear in the editor's top toolbar.

> **Comfortable with a terminal?** You can skip steps 2–3 of this guide entirely. Download the binary for your platform from the [latest release](https://github.com/mrf/godot-stagehand/releases/latest) — `godot-stagehand-linux-amd64`, `godot-stagehand-darwin-arm64` (Apple Silicon), `godot-stagehand-darwin-amd64` (Intel Mac), or `godot-stagehand-windows-amd64.exe` (macOS/Linux: `chmod +x` it first) — then run `godot-stagehand setup /path/to/your/project`. That one command copies the addon, enables the plugin, registers the runtime autoload, and prints the exact Claude config and run command for you. Pass `--force` to overwrite an existing install. If no release exists yet for your platform, see [Build from source](#build-from-source-fallback) below. (The old `./copy-addon.sh` script is deprecated; it now just forwards to `godot-stagehand setup`.)

---

## Step 3: Get the server and configure Claude

With the addon enabled, click the **Setup…** button in the toolbar. This opens the Stagehand Setup wizard, which handles the rest without a terminal:

1. **Server binary** — the wizard detects your OS and shows a destination path (editable, or use **Browse…**). Click **Download server binary**.
   - If it reports no published binary is available for your platform, see [Build from source](#build-from-source-fallback) below.
   - macOS may block the downloaded binary as from an unverified developer — open **System Settings → Privacy & Security**, scroll to the blocked-app notice, and click **Allow Anyway**.
2. **MCP client config** — the wizard shows a JSON snippet with the binary path already filled in. Click **Copy config to clipboard**.
3. Paste that snippet into your Claude client's settings file:

| Claude client | Settings file location |
|--------------|------------------------|
| Claude Desktop (Windows) | `%APPDATA%\Claude\claude_desktop_config.json` |
| Claude Desktop (macOS) | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| Claude Code (Claude CLI) | `.claude/settings.json` inside your project, or `~/.claude/settings.json` globally |

If the file doesn't already have an `mcpServers` section, the pasted snippet looks like this:

```json
{
  "mcpServers": {
    "godot-stagehand": {
      "command": "/path/to/godot-stagehand"
    }
  }
}
```

If it already has other MCP servers configured, add just the `"godot-stagehand": { ... }` entry inside the existing `mcpServers` object instead of overwriting it.

After saving, **restart Claude Desktop** (or reload your Claude Code window). `godot-stagehand` should appear in Claude's available tools.

### Build from source fallback

If no prebuilt binary is available for your platform, build one from source (requires [Go 1.25+](https://go.dev/dl/)):

```bash
git clone https://github.com/mrf/godot-stagehand
cd godot-stagehand
go build -o godot-stagehand .
```

Then point the destination path field in the Setup wizard (or the `command` in your JSON config) at the binary you just built.

---

## Step 4: Run your game with Stagehand enabled

Stagehand is **off by default** — you turn it on when you want it.

### Option A: From the Godot editor

Click the **Stagehand** toggle button in the top toolbar, then press **Play (F5)** as normal. The toggle is editor-only: it adds `--stagehand` to editor play sessions and is not exported with your game.

### Option B: From the command line

```bash
# macOS / Linux
godot --path /path/to/your/project --stagehand

# Windows
"C:\path\to\Godot_v4.x-stable_win64.exe" --path "C:\path\to\your\project" --stagehand
```

> **Installing into an existing project you didn't write?** Many real projects
> (editors, tools, anything with its own `--help`) parse their own command-line
> arguments and will reject an option they don't recognize, printing something
> like `Unknown option: --stagehand` and quitting. Use the environment variable
> instead — it's never seen by argument parsing:
>
> ```bash
> STAGEHAND_ENABLED=1 godot --path /path/to/your/project
> ```
>
> Both forms turn Stagehand on identically. Reach for `--stagehand` for your
> own game (where you control the argv), and `STAGEHAND_ENABLED=1` whenever
> the host project might parse its own flags.

**You're good if:** the Godot output panel (or terminal) shows:
```
Stagehand: Authentication token: <one-session-token>
Stagehand: Server listening on port 26700 (127.0.0.1)
```

Keep that token private. You will give it to Claude in the next step; a new
token is generated each time unless you explicitly set `STAGEHAND_AUTH_TOKEN`.

**Try this if not:** confirm the Stagehand plugin is enabled (Step 2) — an unchecked plugin means neither the toggle, `--stagehand`, nor `STAGEHAND_ENABLED=1` does anything.

You can also click **Test connection** in the Setup wizard (Step 3) once the game is running — it pings the server directly from the editor and reports success or failure without needing Claude at all.

---

## Step 5: Try it — your first command

With your game running, go to Claude and ask it:

> "Connect to my Godot game using auth token `<one-session-token>` and show me the scene tree."

Claude will pass the token to `godot_connect`, then call `godot_get_tree` to read
the scene structure.

**What you should see:**

Claude's response will describe your actual scene tree — the node names and structure will match your project, something like:
```
Connected to Godot 4.x at 127.0.0.1:26700
Scene tree:
└── Node (root)
    └── Main
        ├── Player
        └── UI
```

**If you see "Connection refused":** The game isn't running with Stagehand enabled, or it hasn't finished loading yet. Check the output panel for `Stagehand: Server listening`.

**If you see "Authentication required" or "Authentication failed":** Use the
token printed by the currently running Godot session, not one from an earlier run.

**If you're on Windows with Claude running in WSL:** See [Windows / WSL Setup](windows-setup.md) for networking instructions — you'll need one extra step to bridge the network stacks. If both Godot and Claude run natively on Windows (no WSL involved), the default `127.0.0.1` connection works with no extra setup.

---

## Troubleshooting

Every known failure mode — connection refused, `Unknown option: --stagehand`,
authentication errors, black screenshots, a missing plugin — is collected in one
place: **[Troubleshooting](troubleshooting.md)**.

---

## What's next

- **More tools:** See the [full tool reference](tools.md) — you can click buttons, set properties, wait for signals, record input, and more.
- **Selectors:** Learn how to target specific nodes by name, class, group, or text — see [Selectors guide](selectors.md).
- **Windows / WSL details:** [Windows / WSL Setup](windows-setup.md).
