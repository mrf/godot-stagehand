# Troubleshooting

The canonical list. Back to the [README](../README.md); if you are still
setting up, work through the [Quickstart](quickstart.md) first.

## "Connection refused" or "failed to connect"

The game isn't running with Stagehand enabled, or you have the wrong host/port.

1. Is Godot running? The game window must be open and the scene loaded.
2. Is Stagehand enabled? Look for `Stagehand: Server listening on port 26700` in
   Godot's output. An unchecked plugin means neither the toggle, `--stagehand`,
   nor `STAGEHAND_ENABLED=1` does anything.
3. Are you on Windows with your client in WSL? See [Windows / WSL Setup](windows-setup.md).
4. Port conflict — another instance already on 26700? Run on a different port:
   - Start Godot with `--stagehand-port=26701` after the `--` separator
     (e.g. `godot ... -- --stagehand-port=26701`), or set `STAGEHAND_PORT=26701`
     for a position-independent alternative — the flag before `--` still
     works too, but logs a warning.
   - In Claude: "Connect to my game on port 26701".

## "Unknown option: --stagehand"

The host project parses its own command-line arguments and doesn't recognize
`--stagehand`, so it aborts before Stagehand (which had already started fine)
is usable. Use the environment variable instead — it bypasses argument parsing
entirely:

```bash
STAGEHAND_ENABLED=1 godot --path /path/to/your/project
```

See [Quickstart, Step 4](quickstart.md#step-4-run-your-game-with-stagehand-enabled).

## "Authentication required" or "Authentication failed"

Pass the token printed by the *currently running* Godot session, or its
configured `STAGEHAND_AUTH_TOKEN`, as `godot_connect(auth_token=...)`.
Generated tokens from prior runs do not work.

## "Connection reset"

Godot started but `_process` isn't ticking (common in headless with heavy
scenes). Use a visible window or a lighter scene.

## Screenshots are empty, black, or grey

Stagehand can only capture what Godot actually renders, and headless Godot
renders nothing. Launch with a visible, non-minimized window —
`godot_launch(headless=false, expect_screenshots=true)`. Headless launches are
for structural tools only.

## "Plugin not found", or Stagehand doesn't appear in Project Settings

1. Confirm `addons/stagehand/plugin.cfg` exists in your project folder.
2. In Godot: **Project → Project Settings → Plugins** — if Stagehand isn't
   listed, the folder is in the wrong place.
3. Try: close Project Settings, wait a moment, reopen it.
4. Or just re-run `godot-stagehand setup /path/to/project` — it idempotently
   copies the addon and enables the plugin and autoload.

## Reading a failure

Every failed call comes back as an error, never as a successful-looking result,
and its text names the method, the selector, a stable machine-readable kind, and
what to try next. The kinds and their JSON-RPC codes are listed in the
[error model](error-model.md).
