# Adding a game-testing gate to your Godot CI

Godot's own test tooling — GUT, GdUnit4 — runs your GDScript in isolation.
That's real coverage, but it proves your `Inventory` class adds items
correctly, not that clicking "Start" in the actual running game takes the
player to the actual main menu. Nothing in a typical Godot CI pipeline
launches the game and pokes at it the way a player would, which means the
class of bug that matters most to players — "the button doesn't do
anything" — usually ships to a human playtester first.

[Godot Stagehand](https://github.com/mrf/godot-stagehand) closes that gap
with a scenario runner: a CLI that launches a real Godot build, drives it
through a sequence of actions and assertions, and exits with a stable,
documented code. That exit code is the actual gate — it's what a CI job
branches on to decide "merge" or "block." This post is about wiring that
gate in, sourced from the project's own `.github/workflows/ci.yml`, which
runs exactly this pattern on every push. (For the pixel-diffing half of
Stagehand — baselines, tolerance knobs, screenshot artifacts — see the
companion post, [Visual regression testing for
Godot](visual-regression-testing-for-godot.md). This one is about the
gate mechanics: exit codes, scenario files, and CI wiring.)

## The primitive: a scenario file and one exit code

A scenario is a JSON file describing how to get a Godot session and what
to do with it — launch or connect, then an ordered list of steps. No MCP
client is involved; `godot-stagehand run` executes the file directly:

```bash
godot-stagehand run scenarios/menu-smoke.json --out-dir ci-artifacts
echo $?
```

That exit code is a stable, documented contract — new failure classes get
new numbers, existing ones never change meaning:

| Code | Meaning |
|------|---------|
| 0 | Success — every step passed |
| 1 | Internal — artifact I/O, a malformed response, a runner bug |
| 2 | Usage — bad flags, unknown action, or the scenario failed validation. Nothing was sent to Godot |
| 3 | Connection — could not launch, reach, or authenticate with Godot |
| 4 | Godot error — a well-formed request the addon rejected (node not found, blocked method) |
| 5 | **Assertion failed** — the game was reachable and answered, but an assertion or visual diff did not hold |
| 6 | Timeout — a wait or deadline expired |

For a CI gate, the codes that actually change what you do are 3, 4, and
5, because they're three different failure stories:

- **3 is your test infrastructure**, not your game: Godot didn't start, or
  the port/token was wrong. Retriable, and not a signal about the build.
- **4 is your scenario file**: a selector that no longer matches, or a
  scene structure that changed shape. Fix the scenario, not the game.
- **5 is the one that should block a merge.** The runner reached Godot,
  the game answered, and it answered wrong.

This is the same split `docs/cli.md` calls out for scenario authoring:
"4 means your test is wrong or the scene changed shape, 5 means the game
behaved incorrectly." A CI dashboard that only distinguishes pass/fail
throws that distinction away. The generated `junit.xml` doesn't: an
assertion failure becomes a JUnit `<failure>` (the game behaved wrongly),
while a connection, usage, or timeout problem becomes an `<error>` (the
test never got to render a verdict) — most CI dashboards already triage
those two differently without extra configuration.

## A minimal scenario

This repo's own smoke test is a reasonable template — structural checks
only, deliberately screenshot-free so it needs no display:

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
  ]
}
```

`target.mode: "launch"` with `port: 0` (the default) spawns a private
Godot instance for the run and tears it down afterward — the paved road
for CI, since it can't collide with another job's game. The whole file is
validated *before* Godot launches: an unknown field, a bad selector, or a
screenshot step under `headless: true` fails at exit code 2, before an
engine startup is spent finding out. `assert_property` and
`assert_node_count` are the two assertion actions — they read a value
once and compare it, failing the run at exit 5 on mismatch. Everything
else (`click`, `wait_for_node`, `set_property`, and the rest) is a
pass-through to the same MCP tools, documented in full in
[docs/cli.md](../cli.md#steps).

## Wiring it into GitHub Actions

Here's a runnable job, grounded in this repo's own `scenario-smoke` job in
`.github/workflows/ci.yml` — adapt the Godot-install and cache steps to
however you already provision a Godot binary in your pipeline:

```yaml
name: CI

on:
  pull_request:
    branches: ["main"]

jobs:
  scenario-smoke:
    name: Scenario runner smoke (real Godot)
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7

      - name: Set up Go
        uses: actions/setup-go@v6
        with:
          go-version: "1.25"

      - name: Build godot-stagehand
        run: go install github.com/mrf/godot-stagehand@<pinned-commit-sha>

      - name: Run the smoke scenario
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
          retention-days: 7
```

The runner itself exits nonzero on any failed step, so the "Run the smoke
scenario" step failing is what fails the job — no extra `if:` conditional
is needed to make a bad scenario block the pipeline. `--out-dir` collects
everything you'd want attached to a failed PR check: `report.json` (full
per-step status and timings), `junit.xml` (for the CI dashboard),
`rpc-trace.json` (every RPC, attributed to the step that issued it), and
`godot.log` (the engine's own stdout/stderr). The `upload-artifact` step
runs with `if: always()` specifically so a failing run still leaves those
artifacts attached — that's the difference between "the merge is blocked"
and "the merge is blocked *and I know why*."

Pin the version you install rather than tracking a moving branch. The
scenario/exit-code contract is stable, but a Stagehand upgrade is still a
new binary in your pipeline; pin it the same way you'd pin any other CI
tool, and bump it deliberately.

## Prove the gate isn't a no-op

A CI check that always passes is worse than no check — it's a false sense
of coverage. Before trusting a gate, it's worth confirming it actually
fails when the game is actually broken, not just when your CI YAML has a
typo. This repo does exactly that for its own scenario-smoke job: alongside
the real smoke scenario, CI also runs a scenario that is deliberately
wrong and asserts the job sees **exit 5**, not just "some kind of
failure." A green-only gate wouldn't catch a runner that silently
swallows a failed assertion — the point of the second scenario is to
prove the failure path is live, not just the success path.

The same idea applies to your own gate: keep one intentionally-failing
scenario (a `wait_for_node` for a node you know doesn't exist, say) that
you run separately and expect to see exit 5 from. If it ever stops
failing, your gate stopped gating.

## Debugging a red run

The one-shot CLI commands exist for exactly the moment a scenario job goes
red and you want to look around without re-running the whole pipeline.
Launch the same project locally with the addon enabled, then point the
CLI at the printed port and token:

```bash
export STAGEHAND_AUTH_TOKEN=<printed token>
godot-stagehand tree --port <printed port> --max-depth 3
godot-stagehand find --port <printed port> 'class:Button' --properties text,visible
```

Same selector syntax, same connection flags, as the scenario file — the
CLI and the runner share the same core, so whatever a step is asserting
against is reproducible one command at a time.

## What this gate does not do

Worth being explicit, the same way the visual-regression post is explicit
about pixel diffing: the scenario runner has no opinion about *what* your
game should do. It executes the steps and actions you write and reports
whether they held. Deciding what's worth asserting — which node should
exist, which property should hold which value, which click should lead
where — is entirely your scenario file. Stagehand's contract stops at
"exit 5 means an assertion you wrote did not hold"; it never claims to
know what your game is supposed to do.
