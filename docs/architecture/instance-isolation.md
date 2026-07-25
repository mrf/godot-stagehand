# Instance isolation: `user://` and the `.godot` import cache

Two Stagehand instances of the *same* project used to share two pieces of
mutable state: the project's `res://.godot/` import cache and its `user://`
directory. Ports and instance tokens isolated the *connections*, not the data.
This document records the contract that fixes that, and its limits.

## What Godot actually offers

Godot 4 has **no `--user-data-dir` flag**. The complete option list in the
[command line reference](https://docs.godotengine.org/en/stable/tutorials/editor/command_line_tutorial.html)
contains no user-data option, and the import cache lives inside the project
(`res://.godot/`) with no relocation flag at all. So the two problems need two
different mechanisms.

## `user://` — per-instance, via the documented data-path environment variables

Per [Data paths](https://docs.godotengine.org/en/stable/tutorials/io/data_paths.html),
the user data location is derived from platform environment variables. Each
launch gets a fresh root directory and the child process is pointed inside it:

| Host | Variables set | Isolated |
|------|---------------|----------|
| Linux / *BSD | `XDG_DATA_HOME`, `XDG_CONFIG_HOME`, `XDG_CACHE_HOME` | yes |
| Windows | `APPDATA`, `LOCALAPPDATA` | yes |
| macOS | — (`user://` resolves under `~/Library/Application Support`, no documented override) | **no** |
| Windows `godot.exe` launched from WSL/Linux | — (the Windows build ignores the host's variables) | **no** |

Where isolation is unavailable the launch still proceeds, but `godot_launch`
returns a `warnings` entry naming the reason. Do not run concurrent instances of
one project in those configurations; use `use_custom_user_dir` /
`custom_user_dir_name` per project copy if you must.

Implementation: `internal/launch/userdata.go`.

### Consequences

- The isolated root is a temporary directory removed when the instance is
  killed, so **nothing written to `user://` survives the instance**. Saves,
  settings and logs are per-launch scratch.
- `godot_launch` reports the root as `user_data_dir`.
- Pass `share_user_data: true` (or `launch.Config.ShareUserData`) to opt out and
  use the project's real `user://`. Only safe with a single instance.
- `launch.Config.UserDataDir` pins the root instead of allocating a temporary
  one; a caller-supplied directory is never deleted.

## `.godot` — shared, made safe by an import-once contract

The import cache cannot be relocated, so it is always shared between instances
of one project. That is fine once it is warm: game runs only read it. The
hazard is two processes performing a **cold import into it concurrently**, which
can leave a half-written cache behind — the gray-screen / stale-`.godot` failure
mode.

`Launch` therefore imports before it spawns anything:

1. If `<project>/.godot/.stagehand_imported` exists, skip — the cache is warm.
2. Otherwise take an exclusive lock file in the OS temp directory keyed on the
   absolute project path. This is a deliberate cross-process singleton:
   independent `godot-stagehand` processes (one per MCP client) must contend on
   the same file. A lock older than twice the import budget is assumed to belong
   to a dead process and is stolen.
3. Re-check the stamp — another launch may have imported while we waited.
4. Run `godot --headless --path <project> --import`, which "starts the editor,
   waits for any resources to be imported, and then quits", then write the stamp.

Concurrent launches therefore fan out only *after* exactly one import has
completed. `launch.Config.SkipImport` opts out; `ImportTimeoutMs` bounds the
import (default 120s, deliberately longer than the readiness timeout because a
cold import of a large project dwarfs it).

The stamp lives inside `.godot/`, which Godot regenerates and projects
gitignore, so it never reaches version control. A project already imported by
the editor has no stamp, so the first launch runs one redundant — and fast —
import to establish it.

Implementation: `internal/launch/importonce.go`.

## What this does *not* fix

Isolation of the running `SceneTree`. One game still accepts many clients and
they all drive the same tree; `instance_id` separates instances only within a
single `godot-stagehand` process. Launch your own instance rather than
`godot_connect`-ing to a shared port.
