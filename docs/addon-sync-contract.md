# Addon Sync Contract

`addons/stagehand` is the **only** authoritative copy of the Stagehand
GDScript addon. It's what `assets.go` embeds (`//go:embed all:addons/stagehand`)
into every release binary and what the `setup` subcommand installs into a
target project.

Two other trees carry their own checked-in copy of the same addon:

- `testdata/test_project/addons/stagehand` — the project CI's `gdscript-parse`
  job launches headless to catch strict-mode/parse errors, and that
  `internal/launch`/`internal/mcpserver`/`internal/godotconn` integration
  tests connect to.
- `examples/minimal-game/addons/stagehand` — the example a user gets from a
  plain `git clone`, opened directly in the Godot editor with no build step.

## Why checked-in copies, not a generated/symlinked one

The example must work via `git clone` + "open in Godot" with zero tooling —
no build step, no `go run` — so its addon can't be gitignored-and-generated
at test/build time, and a symlink risks breaking on Windows checkouts without
symlink support. Both fixture copies are therefore real, checked-in files,
kept honest with a byte-for-byte identity test instead.

## The guarantee

`TestFixtureAddonCopiesMatchCanonical` (`addon_copy_drift_test.go`, root
package) hashes every file under `addons/stagehand` and asserts both fixture
copies match exactly — same relative paths, same content, nothing missing,
nothing extra. It runs as part of `go test ./...`, so it's enforced on every
CI run and before every commit per this repo's testing discipline.

## Maintenance

After changing anything under `addons/stagehand`, run:

```bash
bash scripts/sync-addon-copies.sh
```

This overwrites both fixture copies from canonical. Then re-run
`go test ./...` and the headless GDScript parse checks
(`godot --headless --path testdata/test_project --quit`,
`godot --headless --path examples/minimal-game --quit`) to confirm strict-mode
compliance before committing.
