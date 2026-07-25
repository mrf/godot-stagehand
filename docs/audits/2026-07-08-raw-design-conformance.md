# Raw audit report: docs/design.md vs implementation (audit-design agent, 2026-07-08)

DESIGN.md vs implementation audit — godot-stagehand. All cites verified by reading code.

## CONFORMANT
- Protocol: JSON-RPC 2.0 over WebSocket, default port 26700 (`stagehand_server.gd:7`), configurable via `STAGEHAND_PORT` env + `--stagehand-port=` (`stagehand_server.gd:587-596`). MCP over stdio (`server.go:58`).
- Activation guard `STAGEHAND_ENABLED=1` / `--stagehand` present (`stagehand_server.gd:555-562`).
- Selector prefixes path/name/class/group/text/meta/unique + `>>` chaining implemented on both sides (`selector/parse.go:96-121`, `selector_engine.gd:61-109,34-58`).
- GDScript resolution matches design table for path→`get_node`, name→`find_children`, class→`is_class` walk, group→`get_nodes_in_group` (`selector_engine.gd:162-197`).
- 16 of 19 designed MCP tools implemented & registered as spec'd (`server.go:179-210`).

## DEVIATIONS (design says X, code does Y)
1. `godot_launch` — design marks "Phase 2 PENDING" (`docs/design.md:312,525`); actually IMPLEMENTED & registered (`server.go:181`, `tools_launch.go:13`). Doc stale.
2. `godot_wait_for_signal` — design marks "Phase 3 PENDING" (`docs/design.md:282,534`); actually IMPLEMENTED (`server.go:200`, `tools_wait.go:73`, addon `wait_signal` `stagehand_server.gd:333`). Doc stale.
3. wait_for_property param — design: `comparator`, values eq/neq/gt/lt/contains, default "eq" (`docs/design.md:297`). Code: `operator`, REQUIRED (no default), values equals/not_equals/exists/contains/greater_than/less_than (`tools_wait.go:136-140`). Name, value-set, and requiredness all differ.
4. wait_for_node default timeout — design 5000ms (`docs/design.md:278`); code 10000ms and adds undesigned `poll_interval_ms` (`tools_wait.go:30,54`; addon `:465-466`).
5. Multi-instance model — design "Multiple Instances": one Go process ↔ one Godot instance, different ports (`docs/design.md:471-474`). Code: single server manages MANY named instances via `instanceManager` + an `instance_id` param on every tool (`server.go:16-19,27-29`, `instance_manager.go`). Architectural change.
6. GWP wait method — design lists `wait_condition` GWP method (`docs/design.md:95`); code has none, split into `wait_for_node` + `wait_for_property` handlers (`stagehand_server.gd:220-221`).
7. `unique:` resolution — design: `get_node("%"+name)` (`docs/design.md:134`). Code: tree-walk matching node.name pattern + hint_tooltip/placeholder_text/accessible_role, NOT %-unique-name lookup (`selector_engine.gd:229-238,335-352`).
8. Editor plugin — design: "toolbar button to start/stop server" (`docs/design.md:328`). Code: toolbar CheckButton persists ProjectSettings `stagehand/server/enabled` (activation toggle, not start/stop) + a "Setup…" wizard button (`plugin.gd:26-40`).
9. Structural drift vs design's file map (`docs/design.md:387-420`): `change_scene`/`call_method`/`evaluate` in `tools_method.go` (design's `tools_navigation.go` doesn't exist); launcher is a separate `internal/launch/` package, not `internal/godotconn/launcher.go`; addon splits handling into undesigned `property_handler.gd`/`method_handler.gd`/`scene_handler.gd` + `editor/` subdir.

## MISSING (designed, not built)
- Only Phase-3 futures remain unbuilt: AccessKit accessibility-tree integration (`docs/design.md:535`) and GitHub Actions CI/CD integration (`:539`). Everything else listed as PENDING/Phase-2/3 (launch, waits, method/eval, change_scene, text:/meta:/chaining, type_text/mouse_move, wait_for_signal, visual-regression, perf, record/replay) is implemented. No material gaps.

## UNDOCUMENTED (built, absent from design doc)
- MCP tools not in design's tool set: `godot_status` (`tools_status.go:11`), `godot_list_instances` + `godot_disconnect` (`tools_instances.go:11,45`), `godot_touch` (`tools_input.go:213` — design has `input_touch` GWP method but no MCP tool), `godot_screenshot_save_baseline` + `godot_screenshot_diff` (`tools_visual.go:31,45`), `godot_get_performance` + `godot_assert_performance` (`tools_performance.go:11,35`), `godot_record_start`/`godot_record_stop`/`godot_replay` (`tools_record.go:9,34,48`). Total 30 registered tools vs 19 in design (`server.go:179-210`).
- GWP methods not in design: `input_text`, `input_mouse_move`, `get_performance`, `assert_performance`, `record_start`, `record_stop`, `replay` (`stagehand_server.gd:213-227`).
- Third activation path: editor toolbar toggle via ProjectSettings `stagehand/server/enabled` (`stagehand_server.gd:546-548`) — design lists only env var + flag.
- Per-launch instance-token handshake: `STAGEHAND_INSTANCE_TOKEN` echoed by ping (`stagehand_server.gd:275`).
- Port-bind-failure self-quit (exit 70) for game launches (`stagehand_server.gd:60-70`).
- `text=` exact-match selector variant (`TextExact`) beyond design's `text:` (`parse.go:21,104`; `selector_engine.gd:84-88`).
- `rank_for_interaction` click-disambiguation ranking (`selector_engine.gd:393-408`).
- `setup` CLI subcommand + `internal/setup` package already shipped (`main.go:18`, `setup_cmd.go`) — yet `docs/architecture/mcp-vs-cli.md:14` says CLI is future/"no changes needed now" and (`:28`) states "23 handlers" (actual: 30 tools / 24 GWP handlers). Both docs stale.

Note: `godot_launch` param set (design `:312-321`: project_path/scene/godot_path/headless/extra_args) not line-by-line verified against `tools_launch.go`; README implies added params like `expect_screenshots`. Flag as likely-drifted-but-unconfirmed.
