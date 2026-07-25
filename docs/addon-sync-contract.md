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

## `.uid` sidecars are tracked, not ignored

Godot 4.4+ assigns every script/resource a stable UID, cached in a `.uid`
sidecar next to the file, and its own guidance is that these must ship with
the project: a UID minted on one machine and left untracked leaves every
other checkout with `ext_resource` links Godot can't resolve until it
re-imports and silently reassigns fresh ones. Skipping that step is exactly
what produced `WARNING: ext_resource, invalid UID ... using text path
instead` the first time a clean `git clone` of `examples/minimal-game` was
opened — the zero-tooling path this repo can't afford to warn on.

So `testdata/test_project/**/*.uid` and `examples/minimal-game/**/*.uid` are
committed like any other project file (only `.godot/` and `*.import` stay
gitignored). `sync-addon-copies.sh` knows this and preserves each copy's
existing `.uid` sidecars across a resync instead of deleting and letting
Godot mint new ones — the ids are assigned per-project, not derived from
content, so canonical `addons/stagehand` (which has no `project.godot` of its
own and is never itself imported) has no `.uid` files to compare against,
and `TestFixtureAddonCopiesMatchCanonical` deliberately excludes them from
its identity check for that reason.

If `sync-addon-copies.sh` adds a new script to a copy, that file has no
`.uid` yet. Open the project in the editor once (or run the headless
`--editor --quit` import) to let Godot mint one, then commit it alongside the
script.
