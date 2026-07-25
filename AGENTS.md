# Agent Instructions

This project uses **br** (beads_rust) for issue tracking. Run `br robot-docs guide` to get started.

## Quality Standards — MANDATORY

**Every agent session MUST follow strict TDD. No exceptions.**

### TDD Protocol

1. **Write a failing test FIRST** — before any implementation code
   - Go: unit test in `*_test.go` or E2E test in `e2e_test.go`
   - GDScript: GdUnit4 test in `testdata/test_project/test/`
2. **Run the test — verify it fails** for the right reason
3. **Write the minimum code** to make the test pass
4. **Run the full test suite** — `go test ./...` — no regressions
5. **Refactor** if needed, re-run tests
6. **Run `code-simplifier:code-simplifier` agent** before committing

### Quality Gates (run before every commit)

```bash
go vet ./...          # Lint
go test ./...         # All Go tests pass
# If GDScript changed — run the GdUnit4 unit suite (headless):
GODOT_BIN=/path/to/Godot_v4.x-stable_linux.x86_64 ./scripts/run-gdscript-tests.sh
# Or through Go, which parses the JUnit report and fails on any failure/skip:
GODOT_BIN=/path/to/Godot_v4.x-stable_linux.x86_64 go test -tags=gdscript -run TestGdUnitSuite .
# Needs Godot 4.6+: GdUnit4 v6.1.3's CLI runner hangs on 4.4 headless. The addon
# itself still targets 4.3+, which the gdscript-parse CI matrix covers.
# If GDScript changed — strict-mode validation:
timeout 5 ${GODOT_BIN:-godot} --path testdata/test_project --headless --stagehand 2>&1 | grep -E "SCRIPT ERROR|Server listening"
# Must show "Server listening on port 26700" with ZERO "SCRIPT ERROR" lines.
# testdata/test_project has all GDScript warnings elevated to errors — this catches:
#   - Implicitly inferred static types (:= without annotation)
#   - float()/int()/bool()/String() constructors with Variant args
#   - Discarded return values
#   - Variable shadowing
#   - Untyped function parameters
#   - Unsafe property/method access on Variant
# Set GODOT_BIN to your Godot binary (e.g. export GODOT_BIN=/path/to/Godot_v4.x-stable_linux.x86_64)
```

### Rules

- **No skipped tests.** If a test can't run in CI, guard it with a build tag — don't `t.Skip()`.
- **No "TODO: add test later."** The test comes first or the code doesn't land.
- **No hallucinated APIs.** Before using any Godot API in GDScript, verify it exists in the Godot docs or by grepping the engine source. `error_string()`, `node.tree`, and similar hallucinations have burned us before.
- **Validate at the Go layer.** Use `selector.ParseChain()` to validate selectors before sending to Godot. Don't rely on GDScript to catch bad input.
- **Test the addon installs cleanly.** If you modify any `.gd` file, verify the addon doesn't break a host project's compilation.
- **GDScript must be strict-mode compliant.** The addon runs in host projects that may have all warnings elevated to errors. Every `.gd` file must use explicit type annotations, capture all return values, avoid `float()`/`int()`/`bool()`/`String()` constructors on Variant, and never shadow base class properties. Test against `testdata/test_project/` (which has strict warnings enabled) before committing any GDScript changes.
- **GdUnit4 test suites need `@warning_ignore_start("return_value_discarded")`.** GdUnit4's assertions are fluent and return `self`, so every unchained `assert_*()` trips `return_value_discarded=2`. Put the annotation at the top of each test file — it is the one scoped, deliberate relaxation; every other strict warning stays an error. A per-function `@warning_ignore(...)` does **not** cover the function body, only `_start`/`_restore` works.
- **GdUnit4 lifecycle hooks are `before()` / `after()` / `before_test()` / `after_test()`.** There is no `before_each()` — a suite using it silently gets no setup at all (this shipped undetected in `test_command_router.gd` because the suite had never been executed).
- **Free test fixtures with `auto_free()`, not `queue_free()`.** `queue_free` is deferred, so a fixture created in `before_test` survives into the next test and pollutes group/name/meta selector queries with stale nodes.
- **Never reference another addon script's `class_name` directly — `preload()` it instead.** A headless game/CLI launch (`godot --headless --stagehand`) never runs an editor import pass, so `global_script_class_cache.cfg` can be empty and global `class_name` identifiers won't resolve. Every cross-file reference under `addons/stagehand/` uses a `const NAME := preload("res://...")` instead. If the script also declares a `class_name`, name the const in `SCREAMING_SNAKE_CASE` (e.g. `ACCESSIBILITY_TREE`) — a const sharing the exact `class_name` spelling shadows that global identifier, which is its own error under `shadowed_global_identifier=2`. See the rationale block in `addons/stagehand/autoload/stagehand_server.gd` and `docs/gdscript-testing.md`.

## Quick Reference

```bash
br ready              # Find available work
br show <id>          # View issue details
br update <id> --claim  # Claim work atomically
br close <id>         # Complete work
br sync --flush-only  # Push beads data to remote
```

## Non-Interactive Shell Commands

**ALWAYS use non-interactive flags** with file operations to avoid hanging on confirmation prompts.

Shell commands like `cp`, `mv`, and `rm` may be aliased to include `-i` (interactive) mode on some systems, causing the agent to hang indefinitely waiting for y/n input.

**Use these forms instead:**
```bash
# Force overwrite without prompting
cp -f source dest           # NOT: cp source dest
mv -f source dest           # NOT: mv source dest
rm -f file                  # NOT: rm file

# For recursive operations
rm -rf directory            # NOT: rm -r directory
cp -rf source dest          # NOT: cp -r source dest
```

**Other commands that may prompt:**
- `scp` - use `-o BatchMode=yes` for non-interactive
- `ssh` - use `-o BatchMode=yes` to fail instead of prompting
- `apt-get` - use `-y` flag
- `brew` - use `HOMEBREW_NO_AUTO_UPDATE=1` env var

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:ca08a54f -->
## Beads Issue Tracker

This project uses **br (beads_rust)** for issue tracking. Run `br robot-docs guide` to see workflow context and commands.

### Quick Reference

```bash
br ready              # Find available work
br show <id>          # View issue details
br update <id> --claim  # Claim work
br close <id>         # Complete work
```

### Rules

- Use `br` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `br robot-docs guide` for detailed command reference and session close protocol
- Use `br comments` or `br create` for persistent knowledge — do NOT use MEMORY.md files

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   br sync --flush-only
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->
