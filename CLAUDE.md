# Godot Stagehand

MCP server (Go) + Godot addon (GDScript) for external game automation and testing.

## Architecture

- `main.go` — entry point. **No arguments = MCP stdio server** (hard
  compatibility contract); any argument dispatches to `setup`, `--version`, or
  `internal/cli`
- `internal/mcpserver/` — MCP server, tool handlers
- `internal/cli/` — CLI frontend: one-shot commands + `run <scenario>`. A
  sibling of `mcpserver`, not built on it
- `internal/scenario/` — scenario model, validation, runner, JSON/JUnit/trace
  reporters — see `docs/cli.md`
- `internal/gwpop/` — shared action registry: action name → GWP method +
  validated params, typed failure kinds, the blocked-method list. Used by
  `cli`/`scenario`; `mcpserver` deliberately does not route through it
- `internal/visual/` — screenshot decode, baselines, pixel diff (shared by
  `mcpserver` and `scenario`)
- `internal/godotconn/` — WebSocket connection to Godot addon
- `internal/selector/` — Selector parsing and validation
- `addons/stagehand/` — GDScript addon (WebSocket server inside the game); the
  **only** authoritative copy — see `docs/addon-sync-contract.md` before
  editing `testdata/test_project/addons/stagehand` or
  `examples/minimal-game/addons/stagehand` directly
- `testdata/test_project/` — Minimal Godot project for integration tests

## Build & Run

```bash
go build -o godot-stagehand .     # build
go run .                           # run
```

## Test

```bash
go test ./...                      # all tests (no Godot needed)
go test ./internal/selector/       # selector tests only

# Real-Godot scenario runner gate (mirrors CI's scenario-smoke job)
GODOT_BIN=/path/to/godot go test -tags=godot -run '^TestScenarioRunner' .
```

## Lint

```bash
go vet ./...
```

## Conventions

- Go: standard library style, `internal/` for non-exported packages
- GDScript: static typing, GDScript style guide, GdUnit4 for tests
- WebSocket port: 26700 (configurable)
- Activation guard: `STAGEHAND_ENABLED=1` or `--stagehand` CLI flag
- Protocol: JSON-RPC 2.0 over WebSocket (Godot Wire Protocol), version-negotiated at `ping`
- Versions: `internal/version/version.go` is authoritative; bump with `./scripts/set-version.sh` — see `docs/versioning.md`

## Quality Standards

**Strict TDD is mandatory.** See AGENTS.md for the full protocol. Summary:

1. Failing test first — before implementation
2. `go test ./...` must pass before every commit
3. `go vet ./...` must be clean
4. No hallucinated Godot APIs — verify before using
5. Validate selectors in Go with `selector.ParseChain()` before sending to Godot
6. If you change `.gd` files, verify the addon doesn't break host project compilation
7. **GDScript must be strict-mode compliant** — all `.gd` files must pass in projects with warnings-as-errors enabled (no implicit type inference, no discarded return values, no untyped params, etc.)
