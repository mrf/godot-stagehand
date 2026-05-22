# Windows / WSL Setup

When Godot runs on Windows and the MCP server runs in WSL, they're on separate network stacks. You need to bridge that gap so the server can reach Godot's WebSocket port.

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
    "godot_path": "/mnt/c/path/to/Godot_v4.3-stable_win64.exe",
    "project_path": "/home/you/project"
  }
}
```

## Troubleshooting

**"Connection refused" from WSL** — Networking isn't bridged. Try Option A (mirrored mode) or check your firewall rule.

**Godot can't find the project** — UNC paths (`\\wsl.localhost\...`) require recent Windows 11. On older versions, copy your project to a Windows-native path.

**Port conflict** — Another instance on 26700. Set `STAGEHAND_PORT=26701` or pass `--stagehand-port=26701`.
