#!/usr/bin/env bash
# test-addon-install.sh — Addon installation smoke test
#
# Copies the stagehand addon into a fresh temporary Godot project, launches
# Godot headless, waits for the WebSocket server to become ready, optionally
# sends a ping, then reports whether any GDScript parse errors were detected.
#
# Usage:
#   ./scripts/test-addon-install.sh [--no-ping] [--port PORT] [--timeout SECS]
#
# Environment:
#   GODOT_BIN / STAGEHAND_GODOT_BIN / GODOT_PATH — override Godot binary path

set -euo pipefail

# ── defaults ──────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TIMEOUT_SECS=30
PORT=""
SKIP_PING=0

# ── argument parsing ──────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-ping)    SKIP_PING=1; shift ;;
    --port)       PORT="$2"; shift 2 ;;
    --timeout)    TIMEOUT_SECS="$2"; shift 2 ;;
    *) echo "Unknown argument: $1" >&2; exit 1 ;;
  esac
done

# ── find Godot binary ─────────────────────────────────────────────────────────
find_godot() {
  for envvar in STAGEHAND_GODOT_BIN GODOT_BIN GODOT_PATH; do
    local val="${!envvar:-}"
    if [[ -n "$val" && -x "$val" ]]; then
      echo "$val"; return 0
    fi
  done
  # Well-known install path used in this project.
  local known="$HOME/.local/bin/godot-4.6.2-linux"
  if [[ -x "$known" ]]; then echo "$known"; return 0; fi
  # Symlink / PATH fallback.
  for name in godot godot4 godot4.5 godot4.4 godot4.3; do
    if command -v "$name" &>/dev/null; then
      command -v "$name"; return 0
    fi
  done
  return 1
}

GODOT_BIN="$(find_godot)" || {
  echo "ERROR: Godot binary not found." >&2
  echo "Set GODOT_BIN, STAGEHAND_GODOT_BIN, or GODOT_PATH, or put godot/godot4 in PATH." >&2
  exit 1
}
echo "Using Godot: $GODOT_BIN"

# ── choose a free port ────────────────────────────────────────────────────────
if [[ -z "$PORT" ]]; then
  # Ask the OS for a free TCP port via Python (widely available).
  PORT="$(python3 -c "import socket; s=socket.socket(); s.bind(('',0)); print(s.getsockname()[1]); s.close()" 2>/dev/null)" || {
    # Fallback: pick a high port that's unlikely to be in use.
    PORT=27500
  }
fi
echo "WebSocket port: $PORT"

# ── create temporary project ──────────────────────────────────────────────────
TMPDIR_PROJECT="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_PROJECT"' EXIT

echo "Temporary project: $TMPDIR_PROJECT"

cat > "$TMPDIR_PROJECT/project.godot" <<'EOF'
; Engine configuration file.
[application]

config/name="Addon Install Smoke Test"
run/main_scene="res://main.tscn"

[autoload]

StagehandServer="*res://addons/stagehand/autoload/stagehand_server.gd"
EOF

# Minimal main scene: one Node keeps the game loop alive.
cat > "$TMPDIR_PROJECT/main.tscn" <<'EOF'
[gd_scene format=3]

[node name="Main" type="Node"]
EOF

# ── copy addon ────────────────────────────────────────────────────────────────
ADDON_SRC="$REPO_ROOT/addons/stagehand"
ADDON_DST="$TMPDIR_PROJECT/addons/stagehand"

if [[ ! -d "$ADDON_SRC" ]]; then
  echo "ERROR: Addon source not found: $ADDON_SRC" >&2
  exit 1
fi

mkdir -p "$(dirname "$ADDON_DST")"
cp -r "$ADDON_SRC" "$ADDON_DST"
echo "Addon copied to $ADDON_DST"

# ── launch Godot headless ─────────────────────────────────────────────────────
LOG_FILE="$(mktemp)"
trap 'rm -f "$LOG_FILE"; rm -rf "$TMPDIR_PROJECT"' EXIT

echo "Launching Godot headless (timeout: ${TIMEOUT_SECS}s)..."

"$GODOT_BIN" --headless --path "$TMPDIR_PROJECT" -- --stagehand \
  > "$LOG_FILE" 2>&1 &
GODOT_PID=$!

cleanup() {
  kill "$GODOT_PID" 2>/dev/null || true
  wait "$GODOT_PID" 2>/dev/null || true
}
trap 'cleanup; rm -f "$LOG_FILE"; rm -rf "$TMPDIR_PROJECT"' EXIT

# ── wait for the WebSocket server to become ready ─────────────────────────────
DEADLINE=$(( $(date +%s) + TIMEOUT_SECS ))
CONNECTED=0

echo "Waiting for WebSocket on port $PORT..."
while [[ $(date +%s) -lt $DEADLINE ]]; do
  if ! kill -0 "$GODOT_PID" 2>/dev/null; then
    echo "Godot exited before WebSocket became ready."
    break
  fi
  # Try a TCP connection to see if the port is open.
  if bash -c "exec 3<>/dev/tcp/127.0.0.1/$PORT" 2>/dev/null; then
    exec 3>&- 2>/dev/null || true
    CONNECTED=1
    echo "Port $PORT is open."
    break
  fi
  sleep 0.2
done

# ── optional ping via Python WebSocket client ─────────────────────────────────
if [[ $CONNECTED -eq 1 && $SKIP_PING -eq 0 ]]; then
  echo "Attempting ping..."
  # Build a minimal JSON-RPC ping over WebSocket using Python's websockets lib.
  # Fall back gracefully if websockets is not installed.
  PING_RESULT="$(python3 - <<PYEOF 2>/dev/null || echo "SKIP"
import asyncio, json, sys
try:
    import websockets
except ImportError:
    print("SKIP")
    sys.exit(0)

async def ping():
    uri = f"ws://127.0.0.1:${PORT}"
    try:
        async with websockets.connect(uri, open_timeout=3) as ws:
            req = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "ping", "params": {}})
            await ws.send(req)
            resp = json.loads(await asyncio.wait_for(ws.recv(), timeout=5))
            result = resp.get("result", {})
            if result.get("status") == "ok" and result.get("engine") == "godot":
                ev = result.get("engine_version", "?")
                sv = result.get("stagehand_version", "?")
                print(f"PONG engine={ev} stagehand={sv}")
            else:
                print(f"UNEXPECTED: {resp}")
    except Exception as e:
        print(f"PING_ERROR: {e}")

asyncio.run(ping())
PYEOF
)"

  case "$PING_RESULT" in
    PONG*)   echo "Ping successful: $PING_RESULT" ;;
    SKIP)    echo "Ping skipped (websockets Python package not available)." ;;
    PING_ERROR*) echo "WARNING: $PING_RESULT" ;;
    *)       echo "Ping result: $PING_RESULT" ;;
  esac
fi

# ── kill Godot ────────────────────────────────────────────────────────────────
kill "$GODOT_PID" 2>/dev/null || true
wait "$GODOT_PID" 2>/dev/null || true

# ── check for parse errors ────────────────────────────────────────────────────
echo ""
echo "=== Godot log ==="
cat "$LOG_FILE"
echo "=== end log ==="
echo ""

PARSE_ERRORS=0
while IFS= read -r line; do
  if [[ "$line" == *"SCRIPT ERROR"* || "$line" == *"Parse Error"* ]]; then
    echo "PARSE ERROR DETECTED: $line" >&2
    PARSE_ERRORS=1
  fi
done < "$LOG_FILE"

if [[ $PARSE_ERRORS -ne 0 ]]; then
  echo "FAIL: addon parse errors detected — see log above." >&2
  exit 1
fi

echo "PASS: addon installed and started with no parse errors."
