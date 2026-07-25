# Implementation Audit — 2026-07-08

Audit of the build against `docs/design.md`, with focus on stability and multi-instance
support for multiple subagents. Three parallel audit passes (design conformance,
stability, multi-instance), key claims independently re-verified by grep/read in the
main session. Baseline: `go build`, `go vet`, full `go test ./...` all green.

## Verdict

- **Design conformance: strong on substance, the doc is badly stale.** Every designed
  feature except AccessKit integration and CI/CD is built. But the implementation has
  outgrown the doc: 30 registered MCP tools vs 19 designed, and the multi-instance
  architecture (named instances in one server) contradicts the doc's
  one-process-one-instance model (`docs/design.md:470-474`).
- **Stability: one causal chain dominates.** No connection liveness detection + no
  per-call deadlines on most tools + a small stdio worker pool = a frozen Godot can
  wedge the whole MCP server.
- **Multi-instance: solid within one process, but the real target (N subagents = N
  separate MCP processes) is protected only by port+token, with two high-risk
  ergonomic traps.**

## 1. Design conformance

### Conformant
- JSON-RPC 2.0 over WebSocket, port 26700 default, `STAGEHAND_PORT` /
  `--stagehand-port` override (`addons/stagehand/autoload/stagehand_server.gd:587-596`).
- Activation guard `STAGEHAND_ENABLED=1` / `--stagehand` (`stagehand_server.gd:555-562`).
- Selector grammar (path/name/class/group/text/meta/unique + `>>` chaining) on both
  sides (`internal/selector/parse.go:96-121`, `addons/stagehand/core/selector_engine.gd:61-109`).

### Deviations (design says X, code does Y)
| # | Design | Actual | Evidence |
|---|--------|--------|----------|
| D1 | `godot_launch` "Phase 2 PENDING" | Implemented & registered | `docs/design.md:312` vs `internal/mcpserver/tools_launch.go:13`, `server.go:181` |
| D2 | `godot_wait_for_signal` "Phase 3 PENDING" | Implemented | `docs/design.md:282` vs `tools_wait.go:73`, `stagehand_server.gd:333` |
| D3 | wait_for_property `comparator` optional, eq/neq/gt/lt/contains | `operator` **required**, different value set (equals/not_equals/exists/contains/greater_than/less_than) | `docs/design.md:297` vs `tools_wait.go:136-140` |
| D4 | wait_for_node timeout 5000ms | 10000ms + undesigned `poll_interval_ms` | `docs/design.md:278` vs `tools_wait.go:30,54` |
| D5 | One Go process ↔ one Godot instance | Single server manages many named instances via `instance_id` on every tool | `docs/design.md:470-474` vs `server.go:16-29`, `instance_manager.go` |
| D6 | GWP `wait_condition` method | Split into `wait_for_node`/`wait_for_property` | `docs/design.md:95` vs `stagehand_server.gd:220-221` |
| D7 | `unique:` uses `get_node("%"+name)` | Tree-walk name/tooltip/placeholder matching, not %-lookup | `docs/design.md:134` vs `selector_engine.gd:229-238,335-352` |
| D8 | Editor plugin: start/stop toolbar button | Persistent ProjectSettings toggle (`stagehand/server/enabled`) + setup wizard | `docs/design.md:328` vs `addons/stagehand/plugin.gd:26-40` |
| D9 | File map (`tools_navigation.go`, `godotconn/launcher.go`) | `tools_method.go`, separate `internal/launch/` package, addon split into handler files | `docs/design.md:387-420` vs tree |

### Undocumented (built, not in design)
- 11 MCP tools: `godot_status`, `godot_list_instances`, `godot_disconnect`,
  `godot_touch`, `godot_screenshot_save_baseline`, `godot_screenshot_diff`,
  `godot_get_performance`, `godot_assert_performance`, `godot_record_start`,
  `godot_record_stop`, `godot_replay` (`server.go:179-210`).
- Third activation path: editor toggle via ProjectSettings (`stagehand_server.gd:546-548`).
- Per-launch instance token handshake `STAGEHAND_INSTANCE_TOKEN` (`stagehand_server.gd:275`).
- Port-bind-failure self-quit exit 70 (`stagehand_server.gd:60-70`).
- `text=` exact-match selector, `rank_for_interaction` click ranking
  (`parse.go:21,104`, `selector_engine.gd:393-408`).
- `setup` CLI subcommand (`main.go:18`, `setup_cmd.go`) — `docs/architecture/mcp-vs-cli.md`
  still calls the CLI future work and says "23 handlers" (actual: 30 tools / 24 GWP handlers).

### Missing (designed, never built)
- AccessKit accessibility-tree integration (`docs/design.md:535`).
- GitHub Actions CI/CD integration (`docs/design.md:539`).

## 2. Stability

### HIGH
- **S1 — No read deadline or ping/pong keepalive.** `internal/godotconn/conn.go:198`
  blocks on `ws.ReadJSON` with zero `SetReadDeadline`/`SetPongHandler` anywhere in
  `internal/` (grep-verified). A frozen-but-alive game or half-open TCP (WSL network
  hiccup) hangs the read loop forever; reconnect never fires.
- **S2 — Only `wait_*` tools set call timeouts.** `context.WithTimeout` appears only in
  `tools_wait.go:63,116,200` (grep-verified). Every other tool (screenshot, evaluate,
  click, get_tree, …) passes a deadline-less ctx into `callGodotInstance` — with S1,
  each call can block a worker permanently. The risk is even named in the comment at
  `tools_wait.go:11-15` but only patched for waits.
- **S3 — Whole-server wedge.** mcp-go's stdio pool is 5 workers; once hung calls exhaust
  it, processing falls back onto the input-reader goroutine — every tool including
  `godot_status`/`godot_disconnect` stops responding until restart. S1+S2+S3 are one
  causal chain; per-call deadlines in `callGodotInstance` fix all three.
- **S4 — Launched Godots leak on hard kill.** No `SysProcAttr` (`Pdeathsig`/`Setpgid`)
  on the child (`internal/launch/launch.go:124`, grep-verified). Graceful signals are
  handled (`server.go:55-58` defers `closeAll` around mcp-go's signal-aware
  `ServeStdio`), but SIGKILL/panic/OOM leaks ~500MB headless Godot processes that hold
  their ports, blocking the next `assertPortFree`.

### MEDIUM
- **S5 — Reconnect leaks the old socket.** `conn.go:69-72` overwrites `c.ws` without
  closing the previous conn — leaked fd plus a stale ~24MiB peer held in the addon per flap.
- **S6 — Addon accepts unbounded peers, never reaps half-open ones.**
  `stagehand_server.gd:102-116`: each peer reserves 8MiB in + 16MiB out buffers;
  `_poll_clients` reaps only `STATE_CLOSED`, so `STATE_CONNECTING` peers leak.
- **S7 — Infinite reconnect masks a dead instance.** `reconnect.go:64-106` retries
  forever at 5s cap with no give-up state surfaced to `godot_status`.
- **S8 — Addon handler runtime error sends no JSON-RPC response.**
  `stagehand_server.gd:163-183`: an aborting `await handler.call()` (e.g. bad
  `evaluate`) never responds for that id → client-side hang (compounds S1/S2).

### LOW
- `launch.go:152,160`: internal `<-wait` has no timeout (exported `Kill()` does).
- `instance_manager.go:101`: `lr.Kill()` error ignored — silent leak on kill timeout.
- Unauthenticated localhost WS executes arbitrary GDScript — by design, but worth a
  line in the threat model.

### Done well
Per-launch token nonce rejects port squatters; `assertPortFree` fails fast; addon
self-quits on bind failure; clean WS multiplexing (buffered response channels, no
head-of-line blocking, `pending` map cleaned on every exit path); screenshot uses
`process_frame` rather than the hang-prone `frame_post_draw`; instance manager holds
no lock across blocking Kill/Close.

## 3. Multi-instance for multiple subagents

### How it works today
- Each MCP client (each Claude session/subagent) spawns its own `godot-stagehand`
  process; the instance registry is an in-process map (`instance_manager.go:24-31`) —
  **not shared across processes**.
- `instance_id` (default `"default"`) on every tool routes within one process
  (`server.go:16-29,157-170`).
- `godot_launch port=0` picks a free ephemeral port (`tools_launch.go:68-74,118-125`);
  `godot_connect` defaults to **fixed 26700** (`tools_lifecycle.go:20,27`).
- The addon accepts N simultaneous clients into one shared SceneTree
  (`stagehand_server.gd:38-39,102-119`) — no per-client isolation.

### Gaps
- **M1 HIGH — accidental game-sharing.** Two subagents that each call `godot_connect`
  with defaults attach to the *same* game on 26700 and mutate one SceneTree →
  cross-talk and nondeterministic tests. `instance_id` gives isolation only *within*
  one MCP process; nothing steers subagents toward launch-your-own.
- **M2 HIGH — same-project cache/data race.** ~~Isolation is by port only. Two launched
  instances of the same project share `.godot/` import cache and `user://`
  (`launch.go:116` passes only ProjectPath). Concurrent cold-import can corrupt the
  cache (matches the previously-observed gray-screen/stale-cache failure). No
  per-instance `--user-data-dir` or project copy.~~
  **FIXED.** Note the audit's premise was wrong: Godot 4 has no `--user-data-dir`
  flag. `user://` is now isolated per launch via the documented data-path
  environment variables, and the un-relocatable `.godot` cache is made safe by a
  cross-process locked import-once-before-fan-out step. Platform limits and the
  contract are in `docs/architecture/instance-isolation.md`.
- **M3 MED — port TOCTOU with no retry.** `findFreePort` closes its probe listener
  seconds before Godot binds; two concurrent launches can collide. The token check
  turns this into a clean error, but there is no retry loop.
- **M4 MED — no cross-process coordination.** Separate MCP processes can't see each
  other's ports/instances; feeds M1/M3.
- **M5 MED — leak on crash** (same as S4).
- **M6 coverage** — untested: cross-process isolation, concurrent-launch port race,
  token-mismatch e2e, two real Godot instances on two ports, same-project contention.

## 4. Prioritized recommendations

1. **Per-call deadlines in `callGodotInstance` + WS read deadline/keepalive** — one
   change kills S1/S2/S3, the biggest stability lever.
2. **Child process hygiene**: `Setpgid` + `Pdeathsig: SIGKILL` on launched Godot; also
   have the addon self-quit when its last client disconnects for `--stagehand` launches
   (opt-out flag). Fixes S4/M5.
3. **Make launch-your-own the paved road for subagents**: tool descriptions should
   warn that `godot_connect` on the default port may attach to another agent's game;
   consider requiring an explicit `port` when `STAGEHAND_MULTI` (or similar) is set.
   Fixes M1 ergonomically.
4. ~~**Per-instance data isolation**: unique `--user-data-dir` (Godot supports
   `user://` redirection via project override or `XDG_DATA_HOME`) and either a
   pre-import step or documented "import once before fan-out" contract. Fixes M2.~~
   **DONE** — env-var `user://` redirection plus a locked pre-import step. There is
   no `--user-data-dir` flag; see `docs/architecture/instance-isolation.md`.
5. **Retry loop on launch port collision** (bounded, e.g. 3 attempts) — fixes M3
   without cross-process registries; M4 then likely not worth building.
6. **Addon hardening**: respond with JSON-RPC error on handler abort (S8), cap peers +
   reap half-open connections (S6), close old socket on reconnect (S5), surface
   "gave up reconnecting" in `godot_status` (S7).
7. **Refresh the docs**: docs/design.md status markers, tool table (30 tools), the
   multi-instance section, and `docs/architecture/mcp-vs-cli.md` handler count.
8. **New tests**: concurrent-launch race, token-mismatch e2e, two real instances on
   two ports, same-project contention, leak-on-crash.

Raw agent reports: `2026-07-08-raw-design-conformance.md`,
`2026-07-08-raw-stability.md`, `2026-07-08-raw-multi-instance.md` (this directory).
