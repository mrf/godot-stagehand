# godot-stagehand stability & robustness audit

## HIGH

### H1 — No read deadline / ping-pong: frozen-but-alive Godot hangs a worker forever
`internal/godotconn/conn.go:198` (`ws.ReadJSON`) — no `SetReadDeadline`, no ping/pong keepalive anywhere (grep confirms zero `SetReadDeadline`/`SetPongHandler`). If Godot is alive but unresponsive (frozen game loop, half-open TCP after a WSL network hiccup, or a handler that aborts on a runtime error before replying), `ReadJSON` blocks indefinitely and `handleDisconnect`/reconnect never fires. `Call` only returns if the *caller's* ctx has a deadline.
Failure: launch, run any non-wait tool while game is frozen → tool never returns; connection never detects the dead peer.
Fix: add `SetReadDeadline` + gorilla ping/pong keepalive (server-initiated pings on a ticker, refresh read deadline on pong), so a silent peer triggers `handleDisconnect` within N seconds.

### H2 — Only the wait_* tools set a Call timeout; every other tool passes a deadline-less ctx
`internal/mcpserver/tools_wait.go` is the ONLY handler that wraps ctx in `context.WithTimeout` (confirmed by grep). `get_tree, query_nodes, change_scene, call_method, evaluate, screenshot, get_property, set_property, get_game_state, get/assert_performance, record_*, replay, and all input_* (click/key/action/touch/type/mouse_move)` call `callGodotInstance(ctx, …)` with the raw MCP worker ctx, which has NO deadline. Combined with H1, each such call against a frozen Godot blocks its stdio worker permanently. The code's own comment (`tools_wait.go:11-15`) acknowledges the indefinite-hang risk but the fix was applied only to the wait tools.
Fix: default per-call deadline in `callGodotInstance` (e.g. 30s), overridable by the wait tools which legitimately need longer.

### H3 — Whole MCP server can wedge: worker pool is 5, and hung workers are never freed
mcp-go stdio uses a fixed pool of **5** `toolCallWorker`s (`server/stdio.go:403`, default `workerPoolSize:5`). Tool calls queue (size 100); when all 5 workers are stuck (H1/H2) they never recover, the queue backs up, and once full `processMessage` falls back to running the call **synchronously on the input-reader goroutine** (`stdio.go:~626`), blocking all further input. Net effect: ~5 hung calls against a frozen Godot make the entire server unresponsive to *every* tool — including `godot_disconnect`/`godot_status` — until restart.
Fix: same as H2 (bounded per-call deadlines) so workers always drain.

### H4 — Launched Godot processes orphan on non-graceful MCP-server exit
`internal/launch/launch.go:124` `exec.Command(...)` sets no `SysProcAttr` (no `Setpgid`/`Pdeathsig`; grep confirms none). Cleanup is only a `defer s.instances.closeAll()` in `Serve()` (`server.go:57`), which runs only on graceful shutdown. On SIGKILL/panic/crash of the Go server, every launched headless Godot (~500 MB each) is orphaned and keeps holding its port. `assertPortFree` then blocks the next launch on that port until a human kills the zombie.
Fix: `SysProcAttr{Pdeathsig: SIGKILL}` (Linux) and/or a process group killed on shutdown; install a signal handler that runs `closeAll()` on SIGINT/SIGTERM.

## MEDIUM

### M1 — Reconnect abandons the old WebSocket without closing it (fd + addon-peer leak)
`internal/godotconn/conn.go:69-72` `dialWebSocket` overwrites `c.ws = ws`; `handleDisconnect` (`reconnect.go:54-62`) never closes the previous `*websocket.Conn`. Each reconnect leaks the old socket fd (closed only by GC finalizer, eventually) and, on the addon side, leaves a stale `WebSocketPeer` that still holds its **24 MiB** of buffers (see M2) until TCP timeout. Repeated flapping accumulates.
Fix: capture and `ws.Close()` the old conn in `handleDisconnect`/before overwrite.

### M2 — Addon: unbounded peer accept, each peer reserves 24 MiB; half-open peers never reaped
`addons/stagehand/autoload/stagehand_server.gd:102-116` `_accept_new_connections` accepts unlimited peers; each sets `inbound_buffer_size = 8 MiB` + `outbound_buffer_size = 16 MiB` (lines 110-111) = **24 MiB per peer**. `_poll_clients` (121-136) only reaps `STATE_CLOSED`; a peer stuck in `STATE_CONNECTING` (TCP connected, handshake never completed) is never removed. Any local process (or a Go reconnect storm per M1) opening N connections costs 24·N MiB in the game.
Fix: cap concurrent clients; drop peers that don't reach `STATE_OPEN` within a timeout; size buffers per actual payload need.

### M3 — Infinite reconnect masks a permanently-dead Godot
`internal/godotconn/reconnect.go:64-106` retries forever (5s cap). There is no give-up and no state distinguishing "briefly flapping" from "gone for good." Callers get `ErrReconnecting` after a 3s queue wait each time, but the instance stays half-alive indefinitely and the goroutine never exits until `Close`. (Backoff math is correct — `attempt>30` short-circuit prevents `<<` overflow.)
Fix: optional max-retry/deadline → transition to `Disconnected` and surface it via `godot_status`.

### M4 — Addon handler that aborts on a runtime error sends no response
`stagehand_server.gd:163-183` `_dispatch_and_respond` does `await handler.call(params)` with no guard. If a handler hits a GDScript runtime error (e.g. `call_method`/`evaluate`/`set_property` against an unexpected node) it aborts before `_send_to_peer`, so the client gets nothing and hangs (per H1, forever). Not a host-game crash, but a silent no-reply.
Fix: wrap dispatch so any handler that returns null/aborts still emits a JSON-RPC error response for the id.

## LOW

- **L1** `launch.go:151` on dial-failure cleanup does `cmd.Process.Kill()` then unconditional `<-wait`; if Kill fails (already-exited edge) the wait still drains via the buffered chan — ok, but there's no timeout on this particular `<-wait` (the exported `Kill()` has one at :262-266; the internal cleanup paths at :152/:160 do not). A wedged process.Wait would hang launch.
- **L2** `expression_evaluator.gd` runs arbitrary GDScript via `Expression.execute` (documented as intentional; trust boundary = the unauthenticated localhost WS). Informational: no auth on the WS port — any local process can drive/inspect the game. By design, but worth stating in the threat model.
- **L3** `_process` (`stagehand_server.gd:76-80`) polls every frame unconditionally while active. Negligible for the normal 1-client case; only matters if M2 lets many peers accumulate.
- **L4** `instance_manager.closeEntry` ignores `lr.Kill()` error (`instance_manager.go:101`); a Kill timeout leaks a process silently.

## DONE WELL

- Per-launch instance-token nonce (`launch.go:106,194`) proves you connected to the process you spawned, rejecting stale squatters on the port.
- `assertPortFree` pre-launch check (`launch.go:98,232`) fails fast instead of silently attaching to a squatter.
- Addon self-quits with a distinct exit code on bind failure for game/CLI launches, preventing ~500 MB headless zombies (`stagehand_server.gd:56-70`) — and deliberately does NOT self-quit for the editor toggle.
- Good WS multiplexing: `writeMu` is held only for the write, not the response wait (`conn.go:97-99`), so concurrent `Call`s don't head-of-line block; response channels are buffered(1) so delivery never blocks the readLoop.
- `pending` map is cleaned on ctx-cancel, write error, and disconnect — no pending-entry leak; `closeOnce` guards double-close.
- Mutex discipline in `instance_manager`: map mutated under lock, blocking `closeEntry`/`Kill` done *outside* the lock (`add`/`remove`/`closeAll`) — no lock held across blocking I/O.
- wait_* tools compute `timeout_ms + 2s` Go-side deadlines (`tools_wait.go`) to outlast the addon's own timeout.
- Screenshot capture uses `tree.process_frame` (guaranteed in headless) instead of `frame_post_draw` which "can hang indefinitely" — a deliberate anti-hang choice (`screenshot_capture.gd:73-77`).
- Addon surfaces send-buffer failures with an actionable error instead of silently dropping (`stagehand_server.gd:174-199`).
- JSON-RPC parse/validation is defensive: non-Dictionary params warn+ignore (`_params`), malformed requests return proper error responses.
- `go vet ./...` is clean.
