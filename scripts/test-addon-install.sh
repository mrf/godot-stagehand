#!/usr/bin/env bash
# test-addon-install.sh — Addon installation smoke test
#
# Copies the stagehand addon into a fresh temporary Godot project, launches
# Godot headless, waits for the WebSocket server to become ready, optionally
# sends an authenticated ping, then reports whether any GDScript parse errors
# were detected. Python's websockets package is required unless --no-ping is
# used; a missing ping dependency is a failure, never a skipped success.
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
cp -rf "$ADDON_SRC" "$ADDON_DST"
echo "Addon copied to $ADDON_DST"

# ── launch Godot headless ─────────────────────────────────────────────────────
LOG_FILE="$(mktemp)"
trap 'rm -f "$LOG_FILE"; rm -rf "$TMPDIR_PROJECT"' EXIT

echo "Launching Godot headless (timeout: ${TIMEOUT_SECS}s)..."

AUTH_TOKEN="$(od -An -N32 -tx1 /dev/urandom | tr -d '[:space:]')"
if [[ ! "$AUTH_TOKEN" =~ ^[0-9a-f]{64}$ ]]; then
  echo "ERROR: Failed to generate a 256-bit authentication token." >&2
  exit 1
fi

STAGEHAND_PORT="$PORT" STAGEHAND_AUTH_TOKEN="$AUTH_TOKEN" \
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
    CONNECTED=1
    echo "Port $PORT is open."
    break
  fi
  sleep 0.2
done

# ── optional ping via Python WebSocket client ─────────────────────────────────
SMOKE_FAILURE=""
PING_SUCCEEDED=0
if [[ $CONNECTED -ne 1 ]]; then
  SMOKE_FAILURE="WebSocket server did not become ready on port $PORT within ${TIMEOUT_SECS}s"
fi

if [[ $CONNECTED -eq 1 && $SKIP_PING -eq 0 ]]; then
  echo "Attempting authenticated ping..."
  if PING_RESULT="$(python3 - <<PYEOF 2>&1
import asyncio, json, sys
try:
    import websockets
except ImportError as error:
    print(f"websocket client unavailable: {error}", file=sys.stderr)
    sys.exit(2)

async def ping():
    uri = f"ws://127.0.0.1:${PORT}"
    async with websockets.connect(uri, open_timeout=3) as ws:
        authenticate = json.dumps({
            "jsonrpc": "2.0",
            "id": 1,
            "method": "authenticate",
            "params": {"token": "${AUTH_TOKEN}"},
        })
        await ws.send(authenticate)
        auth_response = json.loads(await asyncio.wait_for(ws.recv(), timeout=5))
        if not auth_response.get("result", {}).get("authenticated"):
            raise RuntimeError(f"authentication rejected: {auth_response}")

        request = json.dumps({"jsonrpc": "2.0", "id": 2, "method": "ping", "params": {}})
        await ws.send(request)
        response = json.loads(await asyncio.wait_for(ws.recv(), timeout=5))
        result = response.get("result", {})
        if result.get("status") != "ok" or result.get("engine") != "godot":
            raise RuntimeError(f"unexpected ping response: {response}")
        engine_version = result.get("engine_version", "?")
        stagehand_version = result.get("stagehand_version", "?")
        protocol = result.get("protocol", "?")
        capabilities = ",".join(result.get("capabilities", []))
        print(
            f"PONG engine={engine_version} stagehand={stagehand_version} "
            f"protocol={protocol} capabilities={capabilities}"
        )

try:
    asyncio.run(ping())
except Exception as error:
    print(f"PING_ERROR: {error}", file=sys.stderr)
    sys.exit(1)
PYEOF
)"; then
    case "$PING_RESULT" in
      PONG*)
        PING_SUCCEEDED=1
        echo "Ping successful: $PING_RESULT"
        ;;
      *) SMOKE_FAILURE="authenticated ping failed: $PING_RESULT" ;;
    esac
  else
    SMOKE_FAILURE="authenticated ping failed: $PING_RESULT"
  fi
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

if [[ -n "$SMOKE_FAILURE" ]]; then
  echo "FAIL: $SMOKE_FAILURE" >&2
  exit 1
fi

if [[ $SKIP_PING -eq 0 && $PING_SUCCEEDED -ne 1 ]]; then
  echo "FAIL: authenticated ping did not complete." >&2
  exit 1
fi

if [[ $SKIP_PING -eq 1 ]]; then
  echo "PASS: addon installed and started with no parse errors (--no-ping)."
else
  echo "PASS: addon installed, started, and answered an authenticated ping with no parse errors."
fi
