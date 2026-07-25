# GDScript testing (GdUnit4)

The addon's GDScript is unit-tested with [GdUnit4](https://github.com/MikeSchulze/gdUnit4),
vendored at `testdata/test_project/addons/gdUnit4` (v6.1.3). Suites live in
`testdata/test_project/test/` and run against that project's checked-in copy of
the addon, which `TestFixtureAddonCopiesMatchCanonical` keeps byte-for-byte
identical to canonical `addons/stagehand` (see [addon-sync-contract.md](addon-sync-contract.md)).

## Running

```bash
export GODOT_BIN=~/.local/bin/godot-4.6.2-linux

./scripts/run-gdscript-tests.sh                              # direct
go test -tags=gdscript -run TestGdUnitSuite .                # via Go
```

The shell runner prints `GDUNIT_REPORT=<path to results.xml>` and exits with
GdUnit4's own code (0 pass, 100 test failures, 103/105 startup/parse failure).
`TestGdUnitSuite` (`gdscript_suite_test.go`, build tag `gdscript`) invokes that
runner, parses the JUnit XML, and fails on any failure, error, **or skip** —
the suite contract is that every test passes or fails, nothing is pending. It
also asserts every module's suite actually ran, so deleting one is a test
failure rather than a silent coverage loss.

The `gdscript` build tag keeps a plain `go test ./...` free of any Godot
dependency. CI runs the suite in its own `gdscript-tests` job.

## Godot version floor

The suite requires **Godot 4.6+**. GdUnit4 v6.1.3's CLI runner hangs on
4.4-stable headless — no output and no report after 120s, versus ~6s to
completion on 4.6.2. This is a constraint of the test tooling only; the addon
itself still targets 4.3+, which the separate `gdscript-parse` CI matrix covers.

## Coverage

One suite per module under `addons/stagehand/core` plus the protocol layer:

| Suite | Module |
| --- | --- |
| `test_selector_engine.gd` | `core/selector_engine.gd` |
| `test_tree_serializer.gd` | `core/tree_serializer.gd` |
| `test_input_simulator.gd` | `core/input_simulator.gd` |
| `test_property_handler.gd` | `core/property_handler.gd` |
| `test_method_handler.gd` | `core/method_handler.gd` |
| `test_expression_evaluator.gd` | `core/expression_evaluator.gd` |
| `test_waiter.gd` | `core/waiter.gd` |
| `test_scene_handler.gd` | `core/scene_handler.gd` |
| `test_command_router.gd` | `core/command_router.gd` |
| `test_json_rpc.gd` | `protocol/json_rpc.gd` |

`test_screenshot_capture.gd`, `test_release_assets.gd`,
`test_stagehand_server_bind.gd`, and `test_stagehand_editor_activation.gd`
cover the remaining pieces.

## Writing suites — the strict-mode rules

`testdata/test_project/project.godot` elevates every `gdscript/warnings/*` to an
error, and that applies to test files too. Three things bite:

**1. Fluent assertions discard a return value.** Every GdUnit4 assertion returns
`self` for chaining, so an unchained `assert_*()` trips
`return_value_discarded=2`. Each suite opens with:

```gdscript
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
```

This is the single deliberate relaxation; every other warning stays an error. A
per-function `@warning_ignore("return_value_discarded")` does **not** cover the
function body — only the `_start`/`_restore` form works (verified against
Godot 4.6.2).

**2. Hooks are `before_test()`/`after_test()`, not `before_each()`.** GdUnit4
does not call an unknown method, so a suite using `before_each()` runs with no
setup at all and every test fails on a null fixture.

**3. Free fixtures with `auto_free()`.** `queue_free()` is deferred, so a
fixture built in `before_test` is still in the tree during the *next* test —
which quietly breaks `group:`, `name:`, and `meta:` queries by matching stale
nodes. `auto_free()` releases synchronously at test end.

Other recurring strict-mode requirements: annotate every local (`inferred_declaration`
and `untyped_declaration` are errors, so no bare `:=` on a Variant-typed
expression), avoid `as Dictionary` / `as Array` on a Variant (`unsafe_cast` — assign
to a typed local instead), and do not name a helper `_get`/`_set`, which collide
with `Object`'s virtuals.
