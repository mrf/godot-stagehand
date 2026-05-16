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
# Set GODOT_BIN to your Godot binary (e.g. export GODOT_BIN=~/.local/bin/godot-4.6.2-linux)
```

### Rules

- **No skipped tests.** If a test can't run in CI, guard it with a build tag — don't `t.Skip()`.
- **No "TODO: add test later."** The test comes first or the code doesn't land.
- **No hallucinated APIs.** Before using any Godot API in GDScript, verify it exists in the Godot docs or by grepping the engine source. `error_string()`, `node.tree`, and similar hallucinations have burned us before.
- **Validate at the Go layer.** Use `selector.ParseChain()` to validate selectors before sending to Godot. Don't rely on GDScript to catch bad input.
- **Test the addon installs cleanly.** If you modify any `.gd` file, verify the addon doesn't break a host project's compilation.
- **GDScript must be strict-mode compliant.** The addon runs in host projects that may have all warnings elevated to errors. Every `.gd` file must use explicit type annotations, capture all return values, avoid `float()`/`int()`/`bool()`/`String()` constructors on Variant, and never shadow base class properties. Test against water-wars (which has strict settings) before committing any GDScript changes.

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
