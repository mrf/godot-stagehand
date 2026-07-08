# Multi-Instance Support Audit — godot-stagehand

## HOW IT WORKS (with cites)

- **Process model.** Each MCP client (Claude session/subagent) spawns its OWN `godot-stagehand` process over stdio (`main.go:21-22`, `server.go:56-58` `ServeStdio`). The instance registry is an **in-process Go map** — `instanceManager{ mu sync.RWMutex; entries map[string]*instanceEntry }` (`instance_manager.go:24-31`). It is **not shared across processes**.
- **instance_id routing.** Optional string param on every tool, default `"default"` (`server.go:16-24`; `instanceIDFrom` 22-24). RPCs route via `callGodotInstance(ctx, instanceID, …)` → `requireConnForInstance` → map lookup (`server.go:85-96,157-170`). Omitted with 2 live instances → silently hits `"default"`.
- **Multiple games per process.** `add()` stores by id and atomically closes/replaces any prior entry for that id (`instance_manager.go:34-49`). Two ids → two WebSocket connections to two games.
- **Port allocation.** `godot_launch port=0` → `findFreePort()` binds `:0`, grabs an ephemeral port, closes, returns (`tools_launch.go:68-74,118-125`). `godot_connect` defaults to **fixed 26700** (`tools_lifecycle.go:20-22,27`). Addon reads `STAGEHAND_PORT` env, else 26700 (`stagehand_server.gd:587-596`).
- **Collision defense.** Pre-spawn `assertPortFree` dials the port (`launch.go:98-100,232-241`); a per-launch random token is passed via env, echoed in `ping`, and verified so you can't silently attach to a stale/wrong instance (`launch.go:106-109,192-197,247-252`; addon `stagehand_server.gd:265-276`).
- **Addon = multi-client.** One game accepts N clients: `_clients` dict keyed by `peer_id`, accept loop (`stagehand_server.gd:38-39,102-119`). **No per-client isolation** — every client drives the same shared `SceneTree`.
- **Lifecycle cleanup.** `Serve()` defers `closeAll()` (`server.go:57`); `closeEntry` closes the socket and `Kill()`s the launched process (`instance_manager.go:96-103`). launch/connect remove the old entry for the id first (`tools_launch.go:87`, `tools_lifecycle.go:59`).

## GAPS / RISKS

| # | Sev | Location | Failure scenario |
|---|-----|----------|------------------|
| G1 | **HIGH** | `server.go:48` note; `stagehand_server.gd:102-136` | Design note says "All clients share one Godot game." `instance_id` isolates only WITHIN one process. Two subagents (two processes) that both `godot_connect` to default 26700 attach to the **same** game and both mutate one `SceneTree` with zero isolation → cross-talk, nondeterministic tests. |
| G2 | **HIGH** | `launch.go:116` (`--path` only) | Instances isolated only by port; both use the same `ProjectPath`. Two Godot procs on one project share `.godot/` import cache + `user://`. Concurrent cold-import can corrupt the cache (cf. MEMORY gray-screen note). No per-instance `--user-data-dir`. |
| G3 | **MED** | `tools_launch.go:68-74,118-125` | `findFreePort` closes the listener seconds before the Godot child binds. Two concurrent launches can pick the same ephemeral port; `26700` default in `godot_connect` is unguarded. Token check demotes this to a clean launch error — but there is **no retry** (findFreePort called once). |
| G4 | **MED** | `instance_manager.go:24` | Registry is in-process only. Independent MCP processes can't see each other's reserved ports/instances; no shared lockfile/registry. Feeds G3. |
| G5 | **MED** | `server.go:57`; `launch.go:124-137` | `closeAll()` runs only on graceful `Serve()` return. Child Godot is not started in its own process group / with `Pdeathsig`. A SIGKILLed subagent leaks orphaned headless Godot procs (~500MB each); addon self-quits only on bind failure, not on client disconnect (`stagehand_server.gd:67-69`). |
| G6 | LOW | `instance_manager.go:96-103` | `godot_disconnect` kills the process only when launched (`lr!=nil`); a manual-`connect` instance just closes the socket — the shared game keeps running. Compounds G1. |
| G7 | coverage | tests | Covered: in-process map concurrency (`instance_manager_test.go:169-187`), two-stub routing in ONE Server (`multi_instance_e2e_test.go`), occupied-port reject (`instance_token_test.go:42-80`). **Untested:** cross-process isolation, concurrent `godot_launch` port race, end-to-end token-mismatch path, process leak on crash, same-project data-dir contention, addon serving 2 real clients. |

## RECOMMENDATIONS (ranked)

1. **Resolve & document the isolation model.** The code note ("share one game") contradicts the goal ("each subagent drives its OWN game"). For per-subagent isolation, mandate: each subagent `godot_launch`es its own game on an auto-port and NEVER relies on `godot_connect`'s default 26700. Put a loud warning in the tool descriptions + README. (fixes G1)
2. **Per-instance data isolation** for same-project launches: pass a unique `--user-data-dir` per instance and pre-import the project once (or launch each from an isolated project copy) to avoid concurrent cold-import corruption. (fixes G2)
3. **Make launch robust to port races:** retry `findFreePort`→`Launch` a few times on bind-failure/token-mismatch instead of one-shot; treat token-mismatch as retryable. (fixes G3)
4. **Kill child Godot on MCP exit even on crash:** start `cmd` in its own process group + set `Pdeathsig` (Linux) so a SIGKILLed MCP process doesn't leak Godot zombies. (fixes G5)
5. **Optional cross-process port coordination:** shared `flock`ed registry file under a namespaced dir so independent processes don't pick colliding ports — or lean on token+retry (rec 3) and skip. (addresses G4)
6. **Tests:** concurrent `godot_launch` race; simulated token-mismatch end-to-end; 2 real Godot instances on 2 ports (integration); same-project contention; process-leak-on-crash.
