# Godot Stagehand

MCP server (Go) + Godot addon (GDScript) for external game automation and testing.

## Architecture

- `main.go` — CLI entry point
- `internal/mcpserver/` — MCP server, tool handlers
- `internal/godotconn/` — WebSocket connection to Godot addon
- `internal/selector/` — Selector parsing and validation
- `addons/stagehand/` — GDScript addon (WebSocket server inside the game)
- `testdata/test_project/` — Minimal Godot project for integration tests

## Build & Run

```bash
go build -o godot-stagehand .     # build
go run .                           # run
```

## Test

```bash
go test ./...                      # all tests
go test ./internal/selector/       # selector tests only
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
- Protocol: JSON-RPC 2.0 over WebSocket (Godot Wire Protocol)
