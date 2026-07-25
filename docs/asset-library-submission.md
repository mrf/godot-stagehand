# Godot Asset Library submission

Field-by-field values for submitting the `addons/stagehand` addon to the
[Godot Asset Library](https://godotengine.org/asset-library/asset). This doc
is the deliverable of godot-stagehand-phase3-vrj.14; **actually submitting
the listing requires Mark's Godot account and is a manual, remote step not
performed here.**

## Decisions

**Category: Tools.** The Asset Library has no "Networking" category —
confirmed against the current category list (2D Tools, 3D Tools, Tools,
Shaders, Scripts, Misc, Templates, Demos, Projects, ...). "Tools" is where
comparable dev/testing/automation addons are listed.

**Server binary link: point to the GitHub Releases page, not a specific
asset URL.** Reasons:
- No binary release has been published yet (see README.md "Setup" section —
  build-from-source is the only working install path today; `git tag -l`
  confirms `v0.2.0` exists but `gh release list` returns none). A hardcoded
  asset URL in the listing would 404 until a release ships, and would go
  stale every time the version bumps.
- `https://github.com/mrf/godot-stagehand/releases` always resolves,
  present or future, and GitHub's own "latest" release is what a human
  wants to land on.
- The addon itself already does per-OS/arch asset resolution better than a
  static link could: the in-editor Setup wizard's "Download server binary"
  button (`addons/stagehand/editor/setup_panel.gd`, using
  `addons/stagehand/editor/release_assets.gd`) hits GitHub's
  `/releases/latest/download/<asset>` redirect for the detected OS/arch, so
  once a release exists it fetches the right binary with no manual asset
  picking. The listing text should tell users that button exists rather
  than duplicate its logic as a link.

**"Download server" button in the editor UI: already implemented — nothing
to build.** `plugin.gd`'s toolbar has a "Setup…" button that opens
`StagehandSetupPanel` (`addons/stagehand/editor/setup_panel.gd`), whose
step 1 is "Download server binary". It resolves the correct release asset
name for the host OS/arch via `StagehandReleaseAssets`
(`addons/stagehand/editor/release_assets.gd`), downloads it with
`HTTPRequest`, marks it executable on Unix, and reports a clear "no
published release asset yet — build from source" message if GitHub returns
404 (true today; will resolve itself once binaries are published). Verified
present and working during this ticket — see "Standalone install
verification" below.

## Bug found and fixed during this ticket

Enabling the plugin in a bare Godot project with strict warnings-as-errors
(`gdscript/warnings/*=2`, `exclude_addons=false` — this repo's own
standard, and a config a careful Asset Library user might well run) failed
to load `plugin.gd` and `editor/setup_panel.gd`:

- `plugin.gd` discarded three non-void return values (`Signal.connect()`,
  `PackedStringArray.append()` ×2) — `return_value_discarded` treated as
  error.
- `editor/setup_panel.gd` had the same issue on nine `connect()` calls, plus
  a redundant `const StagehandReleaseAssets := preload(...)` that shadowed
  the global class name `release_assets.gd` already registers via
  `class_name` — a duplicate-symbol parse error.

Root cause this went undetected: CI's `gdscript-parse` job runs
`godot --headless --path testdata/test_project --quit` with no `-e`, which
only parses the `[autoload]` script chain (`stagehand_server.gd` and what it
pulls in) — `plugin.gd` is an `EditorPlugin` script and is only
loaded/parsed when the editor actually activates it (`-e`, or a human
enabling the plugin in Project Settings → Plugins). That path was untested.
Fixed both files (captured every discarded return value in a `_foo_err`
variable per this repo's existing convention; dropped the redundant
`preload`); re-verified clean under `-e` — see below. Not filing this
separately since fixing it was directly in scope ("verify \[standalone
install\] and fix anything that breaks it").

## Standalone install verification

Copied `addons/stagehand/` alone (no other files) into a scratch project
with this repo's strict warnings config
(`gdscript/warnings/*=2`, `exclude_addons=false`) and a trivial main scene,
then ran Godot 4.6.2 headless twice:

```bash
godot --headless --path <scratch> --quit          # game-mode parse, matches CI
godot --headless -e --quit --path <scratch>        # actually activates the EditorPlugin
```

Both exits were `0` with zero `SCRIPT ERROR`/`Parse Error`/warning output
after the fix above. The `-e` run is the one that matters here: it's the
only path that loads `plugin.gd` and exercises `_enter_tree()` (autoload
registration, toolbar button, setup wizard construction) exactly as the
Godot editor would after a user installs the addon from the Asset Library
and enables it — with nothing but `addons/stagehand/` and a project.godot
present, no `go.mod`, no server binary, no other repo files.

`go vet ./...`, `go build .`, and `go test ./...` (which includes
`TestFixtureAddonCopiesMatchCanonical` in `addon_copy_drift_test.go`,
enforcing that `testdata/test_project/addons/stagehand` and
`examples/minimal-game/addons/stagehand` stay byte-identical to the
canonical copy) all pass after running
`bash scripts/sync-addon-copies.sh`.

## Submission form values

| Field | Value |
|---|---|
| Asset Name | **Godot Stagehand** (not bare "Stagehand" — matches the repo, binary, and MCP server name everywhere else; avoids collision with the generic word) |
| Category | Tools |
| Version | `0.2.0` — **see caveat below before submitting** |
| Godot version | `4.3` (minimum). Compatibility table below; re-submit/update if the Asset Library asks for a specific max. |
| License | MIT |
| Repository Host | GitHub |
| Repository URL | `https://github.com/mrf/godot-stagehand` |
| Issues URL | `https://github.com/mrf/godot-stagehand/issues` |
| Download Commit | **See caveat below — do not use the `v0.2.0` tag as-is** |
| Icon URL | `https://raw.githubusercontent.com/mrf/godot-stagehand/<same ref as Download Commit>/addons/stagehand/icon.png` |

### Caveat: don't submit against the stale `v0.2.0` tag

`git tag -l` shows `v0.2.0` (commit `4b3eecb`, 2026-05-29), but `main` has
moved well past it — recent commits include `fix(security): authenticate
Stagehand sessions`, `fix(ci): require authenticated addon smoke`,
`fix(stability): bound Godot RPC liveness`, and
`fix: prevent stagehand activation in release exports`. Submitting the
Asset Library listing pinned to `v0.2.0` would ship an addon missing those
fixes. Before Mark submits:

1. Decide whether this vrj.14 work (and whatever else has landed) warrants
   a version bump — likely yes, given the security fixes above are
   user-facing behavior changes. Bump with `./scripts/set-version.sh
   <version>` per `docs/versioning.md`, which moves `plugin.cfg`'s
   `version=` field too.
2. Tag that commit (`git tag vX.Y.Z`) and push it — a remote op, Mark's to
   do.
3. Use that tag's commit hash (`git rev-parse vX.Y.Z`) as the "Download
   Commit" field, and the same tag name in the Icon URL path above.

Using a moving ref like `main` for either field is not an option — the
Asset Library's "Download Commit" is explicitly a pinned commit, and a raw
GitHub icon URL against a branch would silently change content out from
under the published listing.

### Godot version compatibility (for the listing description / min-max)

Mirrors README.md's "Godot version compatibility" section, tested via
`scripts/test-godot-compat.sh` and CI's `gdscript-parse` matrix job:

| Godot version | Status |
|---|---|
| 4.2 | Not supported (`is not` operator, added in 4.3, fails to parse) |
| 4.3 | Supported (minimum) |
| 4.4 | Supported |
| 4.5 | Supported |
| 4.6 | Supported (local dev baseline) |
| 4.7 | Supported |

### Listing description (paste as-is, fill in the version/commit blanks per the caveat above)

```
Stagehand gives an external process — an AI agent, a CI script, your own
tooling — a live connection to your *running* Godot game: click buttons,
read/set properties, wait for signals, take screenshots, record and replay
input, assert performance counters. Think Playwright, but for a game engine.

IMPORTANT — this addon is one half of Stagehand. It opens a WebSocket
server inside your running game and speaks a JSON-RPC protocol, but it does
not do anything by itself: you also need the godot-stagehand SERVER BINARY,
a small Go program that acts as the MCP server your AI agent or script
actually talks to. Get it from the GitHub Releases page:

  https://github.com/mrf/godot-stagehand/releases

(No prebuilt binaries published as of this listing's Godot version/commit —
build from source with `go build .` in the meantime; see the repo README.)

Once this addon is enabled in your project, open its toolbar's "Setup…"
button for a wizard that downloads the matching server binary for your OS,
prints the MCP client config snippet, and tests the connection — no manual
asset picking required once binaries are published.

Full docs, the quickstart guide, and the security model (auth tokens,
localhost-only by default, activation requires --stagehand so it never runs
in release exports) are in the repository README:

  https://github.com/mrf/godot-stagehand

License: MIT.
```

## Files touched for this submission

- `addons/stagehand/plugin.cfg` — description now states the two-piece
  setup and links GitHub Releases (all other required fields — name,
  author, version, script — were already present and valid; Godot's
  `plugin.cfg` format has no icon or version-range field, so those live
  only in this doc / the submission form).
- `addons/stagehand/icon.png` — new, 256×256 PNG (exceeds the 128×128
  minimum), square, generated to depict a marionette control bar (a
  "stagehand" pulling strings) in the same blue accent used by the setup
  wizard's "connected" state.
- `addons/stagehand/plugin.gd`, `addons/stagehand/editor/setup_panel.gd` —
  bugfixes above.
- `testdata/test_project/addons/stagehand`,
  `examples/minimal-game/addons/stagehand` — re-synced from canonical via
  `scripts/sync-addon-copies.sh` per `docs/addon-sync-contract.md`.
- `LICENSE` — already present at repo root (MIT), reachable by anyone who
  installs just `addons/stagehand/` since the Asset Library links back to
  the repository regardless of which subdirectory the addon lives in.
