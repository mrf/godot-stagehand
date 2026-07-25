# CLI and scenario runner

Stagehand ships two frontends over the same core. The MCP server is for AI
agents; the CLI is for CI pipelines and for poking at a running game from a
terminal. Neither is built on the other — both talk to `internal/godotconn`
through the shared operation registry in `internal/gwpop`.

```
godot-stagehand                       # MCP stdio server (unchanged default)
godot-stagehand serve                 # the same, explicitly
godot-stagehand setup <project>       # install the addon
godot-stagehand <command> [flags]     # one-shot command against a running game
godot-stagehand run <scenario.json>   # run a scenario end to end
```

**The no-argument invocation is still the MCP stdio server.** Existing MCP
client configurations keep working untouched; a test drives the built binary
through a real `initialize` handshake to keep it that way.

## Exit codes

These are a stable contract. A CI gate branches on them, so a code's meaning
never changes; new failure classes get new numbers.

| Code | Meaning |
|------|---------|
| 0 | Success — the command ran, or every scenario step passed |
| 1 | Internal — artifact I/O, a malformed response, a runner bug |
| 2 | Usage — bad flags, unknown action, scenario failed validation. Nothing was sent to Godot |
| 3 | Connection — could not launch, reach, or authenticate with Godot |
| 4 | Godot error — a well-formed request the addon rejected (node not found, blocked method) |
| 5 | **Assertion failed** — the game was reachable and answered, but an assertion or visual diff did not hold. This is the "real regression" code |
| 6 | Timeout — a wait or deadline expired |

The 4/5 split is the useful one: 4 means your test is wrong or the scene
changed shape, 5 means the game behaved incorrectly.

## One-shot commands

Every game-touching command takes the same connection flags:

```
--host string       Godot host (default 127.0.0.1)
--port int          WebSocket port — REQUIRED
--token string      session secret (default $STAGEHAND_AUTH_TOKEN)
--timeout duration  bound the whole command (default 30s)
```

`--port` has no default on purpose. The addon's shared default 26700 routinely
belongs to a game somebody else launched, and silently driving another agent's
SceneTree is worse than a usage error. Pass the port your instance printed at
startup.

Flags may appear before or after positional arguments. A positional that
genuinely starts with `-` must follow a `--` terminator.

```bash
export STAGEHAND_AUTH_TOKEN=<token this Godot session printed>

godot-stagehand connect     --port 26788
godot-stagehand status      --port 26788
godot-stagehand tree        --port 26788 --max-depth 3 --properties text,visible
godot-stagehand find        --port 26788 'class:Button' --properties text
godot-stagehand property get --port 26788 'name:titleLabel' text
godot-stagehand property set --port 26788 'name:statusLabel' text 'hello'
godot-stagehand call        --port 26788 'name:Player' take_damage --args '[5]'
godot-stagehand eval        --port 26788 'Engine.get_frames_per_second()'
godot-stagehand scene       --port 26788 res://scenes/main_menu.tscn

godot-stagehand input click  --port 26788 --selector 'text:Start'
godot-stagehand input click  --port 26788 --position '{"x":100,"y":200}'
godot-stagehand input key    --port 26788 Enter --modifiers ctrl,shift
godot-stagehand input action --port 26788 ui_accept
godot-stagehand input text   --port 26788 'hello' --selector 'class:LineEdit'
godot-stagehand input move   --port 26788 --coordinates '{"x":10,"y":10}'
godot-stagehand input touch  --port 26788 --position '{"x":10,"y":20}' --drag-to '{"x":90,"y":20}'

godot-stagehand wait node     --port 26788 'name:Hud' --state visible --timeout-ms 5000
godot-stagehand wait signal   --port 26788 'name:Hud' ready
godot-stagehand wait property --port 26788 'name:Hud' visible --operator equals --expected true

godot-stagehand screenshot  --port 26788 --out frame.png
godot-stagehand screenshot  --port 26788 --baseline main_menu
godot-stagehand screenshot  --port 26788 --diff main_menu --threshold 0.01

godot-stagehand performance --port 26788 --monitors TIME_FPS,MEMORY_STATIC
godot-stagehand performance --port 26788 --assert TIME_FPS --threshold 55 --op gte
godot-stagehand performance --port 26788 --assert TIME_FPS --threshold 55 --op gte \
  --warmup-ms 500 --sample-count 30 --sample-interval-ms 16 --statistic p95
```

Every command prints JSON on stdout, so `jq` works throughout. `property set`
and `wait property --expected` parse their value as JSON with a string
fallback: `3` is the number, `hello` is the string, `'"3"'` is the string `"3"`.

`godot-stagehand actions` lists every action a scenario step may use, with its
required and optional parameters — generated from the same table that
validates scenarios, so it cannot drift.

## Scenarios

A scenario is a JSON file describing how to obtain a Godot session and what to
do with it. `godot-stagehand run` executes it with no MCP client involved.

```json
{
  "name": "menu smoke",
  "description": "The main menu loads and the start button works",
  "target": {
    "mode": "launch",
    "project_path": "../my-game",
    "headless": true,
    "timeout_ms": 60000
  },
  "steps": [
    { "name": "menu appears", "action": "wait_for_node",
      "with": { "selector": "name:MainMenu", "timeout_ms": 15000 } },
    { "action": "assert_property",
      "with": { "selector": "name:titleLabel", "property": "text",
                "operator": "equals", "expected": "My Game" } },
    { "action": "click", "with": { "selector": "text:Start" } },
    { "action": "wait_for_node",
      "with": { "selector": "name:Hud", "state": "visible", "timeout_ms": 10000 } }
  ],
  "teardown": [
    { "action": "change_scene", "with": { "scene_path": "res://scenes/main_menu.tscn" } }
  ]
}
```

Run it:

```bash
godot-stagehand run scenarios/menu-smoke.json --out-dir ci-artifacts
echo $?   # 0 pass, 5 an assertion failed, 3 could not reach Godot, ...
```

### Structure

| Field | Meaning |
|-------|---------|
| `name` | Report and JUnit suite name. Defaults to the file stem |
| `description` | Carried into the report |
| `target` | How to obtain the session (below) |
| `baseline_dir` | Screenshot baselines. Relative to the scenario file. Default `stagehand-baselines` |
| `steps` | Ordered operations. The first failure stops the run |
| `teardown` | Always runs, including after a failure |

Unknown fields are rejected at parse time. A typo must fail loudly rather than
silently skip a step nobody notices is missing. The whole file — every step,
every parameter, every selector — is validated *before* Godot is launched, so
an authoring mistake in the last step does not cost a full engine startup.

### Target

`"mode": "launch"` spawns a private instance for the run and kills it
afterwards. This is the paved road for CI: port `0` (the default) auto-assigns,
so it cannot collide with another agent's game.

| Field | Notes |
|-------|-------|
| `project_path` | Required. Relative to the scenario file |
| `godot_bin` | Overridden by `--godot-bin` |
| `headless` | Default `true`. Screenshot steps require `false` |
| `allow_unsafe` | Enables `evaluate` and arbitrary `call_method` |
| `share_user_data` | Opts out of per-instance `user://` isolation |
| `extra_args` | Passed to the game after `--` |
| `port`, `host`, `timeout_ms` | |

`"mode": "connect"` attaches to an already-running game. `port` is required —
the shared default is refused for the same reason the CLI requires `--port`.
Supply the secret via `token_env` (preferred), `token`, `--token`, or
`STAGEHAND_AUTH_TOKEN`; never check a token into a scenario file.

### Steps

```json
{ "name": "optional label", "action": "click",
  "with": { "selector": "text:Start" },
  "continue_on_failure": false }
```

Parameters live under `with` so they can never collide with a step field.
`continue_on_failure` records the failure and keeps going — use it for
best-effort probes, not for real assertions.

Actions fall into three groups:

- **Pass-through** — `tree`, `find`, `get_property`, `set_property`,
  `call_method`, `evaluate`, `change_scene`, `click`, `press_key`,
  `press_action`, `type_text`, `mouse_move`, `touch`, `wait_for_node`,
  `wait_for_signal`, `wait_for_property`, `get_performance`,
  `assert_performance`, `record_start`, `record_stop`, `replay`, `ping`,
  `game_state`. Parameters match the MCP tool arguments.
- **Assertions** — `assert_property` and `assert_node_count` read once and
  compare, failing the run with exit 5. `assert_performance` fails the step
  when the addon's verdict is false; it defaults to one instantaneous sample,
  but accepts `warmup_ms`, `sample_count` (or `duration_ms` +
  `sample_interval_ms`), and a `statistic` (`min`, `max`, `mean`, `median`,
  `p95`) to sample and assert monitors over time instead — see the
  performance-monitoring note in the [README](../README.md) for what this is
  not yet: proven statistical regression gating.
- **Local** — `sleep`, `screenshot`, `save_baseline`, `screenshot_diff`.

The `name` of a `save_baseline` / `screenshot_diff` step is a filename stem,
not a path, and is checked at validation time against the allowlist in
[visual-regression.md](visual-regression.md#name-allowlist) — separators,
absolute paths, dot segments and control characters are rejected.

Comparison operators: `equals`, `not_equals`, `contains`, `exists`,
`greater_than`, `less_than`. Ordering comparisons on non-numbers are an
authoring error, not a silent false — a silently-false assertion reads as a
real regression.

`screenshot`'s `output` and the baseline `name` may not escape the run's
directories. Scenario files are data and may arrive in a pull request.

### Artifacts

`--out-dir DIR` writes everything a failing CI job needs:

| Path | Contents |
|------|----------|
| `report.json` | Full run report: per-step status, timings, results, failure classification |
| `junit.xml` | JUnit XML for the CI dashboard |
| `rpc-trace.json` | Every RPC with its duration, attributed to the step that issued it |
| `godot.log` | The engine's own stdout/stderr (launch mode) |
| `screenshots/` | Frames written by `screenshot` steps |
| `diffs/` | `<name>-actual.png` and `<name>-diff.png` for failed comparisons |
| `<baseline_dir>/` | Baselines written by `save_baseline` steps |

`--json` and `--junit` override the report locations individually. The JSON
report also goes to stdout so a pipeline can consume it without an out-dir;
progress and the summary go to stderr, so the two never mix.

In JUnit terms, an assertion failure is a `<failure>` (the game behaved wrongly)
while a connection, usage or timeout problem is an `<error>` (the test never got
to judge the game). CI dashboards triage those differently.

## Using it in CI

```yaml
- name: Stagehand smoke
  run: |
    godot-stagehand run scenarios/menu-smoke.json \
      --out-dir stagehand-artifacts \
      --godot-bin "$GODOT_BIN"

- name: Upload Stagehand artifacts
  if: always()
  uses: actions/upload-artifact@v7
  with:
    name: stagehand-artifacts
    path: stagehand-artifacts/
```

The scenario runner exits nonzero on failure, so no extra `if` is needed to
fail the build. `.github/workflows/ci.yml`'s `scenario-smoke` job runs exactly
this against `testdata/scenarios/smoke.json` on every push, plus a
deliberately-wrong scenario that must exit 5 — a green-only gate would not
catch a runner that swallows failures.

**Screenshots need a rendered window.** Headless Godot cannot produce a real
frame, so a scenario with `save_baseline` or `screenshot_diff` steps is
rejected at validation time when `headless` is true. For a visual gate in CI,
set `"headless": false` and provide a display (Xvfb). See
[visual-smoke-contract.md](visual-smoke-contract.md).

## Debugging from a terminal

The one-shot commands exist for the moment a scenario fails and you want to
look around. Launch your game with the addon, note the port and token it
prints, then:

```bash
export STAGEHAND_AUTH_TOKEN=<printed token>
godot-stagehand tree --port <printed port> --max-depth 3
godot-stagehand find --port <printed port> 'class:Button' --properties text,visible
```

Selector syntax is identical everywhere — see [selectors guide](selectors.md).
