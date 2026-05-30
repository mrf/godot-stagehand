# Windows / WSL Setup

When Godot runs on Windows and the MCP server runs in WSL, they may be on separate network stacks. Use an explicit host so the server reaches Godot's WebSocket port deterministically.

## Host Selection

| Where Godot runs | Where `godot-stagehand` runs | Host to use |
|------------------|------------------------------|-------------|
| Linux/macOS/local same OS | Same OS | `127.0.0.1` |
| Linux Godot inside WSL | Same WSL distro | `127.0.0.1` |
| Windows Godot, WSL mirrored networking | WSL | `localhost` |
| Windows Godot, WSL NAT/default networking | WSL | WSL default gateway IP |

## Networking

**Option A: Mirrored networking (recommended)**

Create `C:\Users\<you>\.wslconfig`:
```ini
[wsl2]
networkingMode=mirrored
```
Restart WSL (`wsl --shutdown`). After this, `localhost` in WSL reaches Windows ports directly — no firewall changes needed.

**Option B: Firewall rule**

Run in PowerShell as Administrator:
```powershell
New-NetFirewallRule -DisplayName "Stagehand Godot" -Direction Inbound -LocalPort 26700 -Protocol TCP -Action Allow
```

Then connect with the Windows host IP:
```json
{ "name": "godot_connect", "arguments": { "host": "172.x.x.x" } }
```
Find your WSL gateway IP with: `ip route show default | awk '{print $3}'`

## Launching Godot from WSL

Godot needs a Windows-native path or a WSL UNC path:

```cmd
godot.exe --path "\\wsl.localhost\Ubuntu\home\you\project" --stagehand
```

Or use `godot_launch` with explicit paths:
```json
{
  "name": "godot_launch",
  "arguments": {
    "godot_bin": "/mnt/c/path/to/Godot_v4.3-stable_win64.exe",
    "project_path": "/home/you/project",
    "host": "localhost"
  }
}
```

For screenshot, baseline, or diff workflows, launch a visible Godot window:

```json
{
  "name": "godot_launch",
  "arguments": {
    "project_path": "/home/you/project",
    "headless": false,
    "expect_screenshots": true
  }
}
```

`expect_screenshots=true` rejects `headless=true` because headless/no-display sessions are only reliable for structural tools such as tree, selector, property, and expression checks.

## Troubleshooting

**"Connection refused" from WSL** — Networking isn't bridged. Try Option A (mirrored mode) or check your firewall rule.

**Godot can't find the project** — UNC paths (`\\wsl.localhost\...`) require recent Windows 11. On older versions, copy your project to a Windows-native path.

**Screenshots are empty, black, or grey** — Use a visible, non-minimized Godot window with `headless=false`. Headless mode is not a supported visual-smoke path.

**Port conflict** — Another instance on 26700. Set `STAGEHAND_PORT=26701` or pass `--stagehand-port=26701`.
