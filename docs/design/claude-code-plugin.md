# Claude Code plugin

**Status:** signed off by the owner 2026-10-03. The mod spike (step 6) found
two conflicts with D7. The owner decided both on 2026-10-03: an observe-only
band, and guidance carried in the JSON. D7 is revised to match, and
implementation continues.

**Owner walkthrough 2026-10-03:** D1–D6 accepted as recommended. Q1 and Q3
decided (see Open questions). A mod is added to v1 (D7) with the full strict
TypeScript gate.

**Issue:** none filed yet.

## Why

A Claude Code user who wants Stagehand today wires three things by hand:

1. Download the right release binary and `chmod +x` it.
2. Paste an `mcpServers` entry with an absolute path to that binary into their
   client config.
3. Find `skills/stagehand.md` and copy it into a `.claude/skills/` directory
   (`docs/tools.md` tells them to).

Step 2 also goes stale every time they upgrade the binary to a new path, and
the skill they copied can drift from the tool set the binary actually has.

A Claude Code [plugin](https://code.claude.com/docs/en/plugins/overview)
packages an MCP server, skills, and command-line tools as one unit. Users
install that unit with one command and get updates from a marketplace. This
repo can be its own marketplace, so the plugin ships and versions together
with the binary and the addon.

The target install becomes:

```text
/plugin marketplace add mrf/godot-stagehand
/plugin install godot-stagehand@godot-stagehand
```

Then the user says "set up Stagehand in this project". The skill runs
`godot-stagehand setup .`, and the MCP tools are already connected. While
Claude plays, a band above the prompt names the connected game, and
`/stagehand-view` opens a pane showing the last frame Claude captured (D7).

## What ships

| Component | v1 | Notes |
| --- | --- | --- |
| MCP server | yes | Declared inline in `plugin.json`. The command is a launcher that resolves a version-matched binary (D3) |
| Skill `stagehand` | yes | The existing `skills/stagehand.md`, moved to `SKILL.md` layout (D5). Runs as `/godot-stagehand:stagehand` |
| `bin/godot-stagehand` | yes | The same launcher. A plugin's `bin/` is on the Bash tool's `PATH` while the plugin is enabled, so `godot-stagehand setup .` and `godot-stagehand run <scenario>` work with no separate install |
| `userConfig.binary_path` | yes | Optional override that points at a locally built binary (D6) |
| [Mod](https://code.claude.com/docs/en/plugins/mods/overview) | yes | TypeScript hooks module: status band above the prompt and a `/stagehand-view` game pane. Additive only; the plugin works without it (D7) |
| Agents | no | A playtester/QA agent is opinionated about workflow. The skill already teaches the workflow. Revisit if people ask |
| Settings hooks, monitors, LSP | no | Nothing to hook. A SessionStart prefetch settings hook is held in reserve for Q3 |

**Not changing:** the Godot addon, `setup`, the MCP stdio contract (no
arguments means stdio server), or the release asset names. One additive MCP
change: `godot_status` gains structured content next to its unchanged text
(D7). Users of other MCP clients keep the current manual path, and the README
keeps documenting it.

## Layout

```text
.claude-plugin/
  marketplace.json                 # repo root = marketplace root
integrations/claude-code/          # plugin root
  .claude-plugin/
    plugin.json
  skills/
    stagehand/
      SKILL.md                     # moved from skills/stagehand.md
  bin/
    godot-stagehand                # POSIX sh launcher (MCP command + CLI shim)
  hooks/
    hooks.json                     # { "modules": ["./register.ts"] }
    register.ts                    # the mod (D7)
    register.test.ts               # run by `claude plugin test`
tools/claude-mod/                  # dev-only TS toolchain; never ships
  package.json                     # devDependencies only
  package-lock.json
  tsconfig.json
  eslint.config.mjs
  gen-types.sh                     # writes the mod's types, no sign-in (Q10)
  check-calls.mjs                  # the D7 calls-allowlist gate
  go.mod                           # fences node_modules off `go test ./...`
```

The mod's tests ship inside the plugin, because `claude plugin test` runs
test files from within the plugin directory. They are small, and they never
load in a session. The toolchain stays outside the plugin root so
`git-subdir` never fetches it.

## Decisions

Each item gives a recommendation and the alternative it beat. Every one is an
owner call.

### D1. The plugin lives in `integrations/claude-code/`, not at the repo root

The plugin root is scanned by convention for `skills/`, `agents/`, `bin/`,
`hooks/hooks.json`, `.mcp.json`, `.lsp.json`, `settings.json` and more. At the
repo root, any future contributor-facing `.mcp.json` or `bin/` would leak into
every user's install. Installs would also copy the whole tracked tree (about
4 MB of Go source, fixtures and baselines) into each user's plugin cache. A
subdirectory isolates the plugin, and `git-subdir` (D2) fetches only that
directory.

*Alternative:* plugin root = repo root, with the marketplace entry listing
`skills: ["./skills/stagehand"]`. That has fewer moving parts and keeps the
skill where it is today. It loses on the leak risk above.

### D2. The marketplace entry is a `git-subdir` source pinned to the release tag

```json
{
  "name": "godot-stagehand",
  "owner": { "name": "mrf" },
  "description": "Drive a running Godot game from Claude Code",
  "plugins": [
    {
      "name": "godot-stagehand",
      "description": "MCP server, agent skill and CLI for automating a running Godot game",
      "source": {
        "source": "git-subdir",
        "url": "mrf/godot-stagehand",
        "path": "integrations/claude-code",
        "ref": "v0.4.1"
      }
    }
  ]
}
```

Pinning matters because of skew. With a relative-path source (`"./integrations/claude-code"`),
users get whatever `main` holds at install time under the current version label.
A skill edit on `main` that documents a tool added after the last release would
reach users before the binary that implements it. A `ref` pinned to the tag
makes the plugin files, `plugin.json`'s `version`, and the downloaded binary
all the same release.

The bump commit sets `ref` to the tag that will point at that same commit, so
there is no chicken-and-egg problem. The one window is between pushing the
commit and pushing the tag. Pushing both at once (`git push --atomic origin
main vX.Y.Z`) closes it, and that becomes a release-checklist step.

*Alternative:* a relative path, which is simpler and lets `/plugin` show the
`plugin.json` description before install. It loses on skew. The entry carries
its own `description`, so the git-subdir listing still reads well.

### D3. The binary comes from a launcher that fetches the version-matched release asset

`plugin.json` declares:

```json
"mcpServers": {
  "stagehand": {
    "command": "${CLAUDE_PLUGIN_ROOT}/bin/godot-stagehand",
    "env": { "STAGEHAND_BINARY": "${user_config.binary_path}" }
  }
}
```

The launcher does the following, in order:

1. If `STAGEHAND_BINARY` is non-empty, exec it.
2. Pick a cache directory: `$CLAUDE_PLUGIN_DATA` when set (it is set for MCP
   servers). Otherwise derive it from the launcher's own path, because Claude
   Code does not export plugin variables to Bash-tool commands. The cache
   layout `<config>/plugins/cache/<marketplace>/<plugin>/<version>/` maps to
   `<config>/plugins/data/<plugin>-<marketplace>/`; that mapping is observed on
   a live install, not documented. If neither applies (for example
   `--plugin-dir` development), fall back to
   `${XDG_CACHE_HOME:-$HOME/.cache}/godot-stagehand`.
3. Map `uname -s`/`uname -m` to the release asset name, using the same matrix
   as `addons/stagehand/editor/release_assets.gd`.
4. If `<cache>/bin/<VERSION>/<asset>` exists and is executable, exec it with
   `"$@"`.
5. Otherwise download `releases/download/v<VERSION>/<asset>` and `SHA256SUMS`
   into a temp file in the same directory. Verify the checksum and
   `chmod +x`. Check that `--version` prints `godot-stagehand <VERSION>`.
   Then `mv` it into place (atomic, so two sessions racing is safe) and exec.
   Remove other version directories after a successful install.

Constraints:

- **stdout stays clean until exec.** It carries the MCP stdio protocol, so all
  launcher output goes to stderr (`curl -fsSL`, no progress bar).
- **Errors name a fix.** A 404 means the tag's assets are not published yet:
  retry in a few minutes, or set `binary_path`. No `curl`/`wget` or no
  `sha256sum`/`shasum` means "install X, or set `binary_path`".
- `VERSION` is a constant in the launcher, rewritten by `set-version.sh` (see
  Versioning).

*Alternative B: an MCP bundle.* `mcpServers` accepts an `https://` URL to a
`.mcpb` bundle, which Claude Code downloads and extracts itself. One bundle
holding all four binaries, with per-platform overrides, would remove the
launcher and cover native Windows (Q1). It loses for now on two unknowns.
First, whether Claude Code honours `.mcpb` platform overrides. Second, the
bundle would be about 4 × 13 MB per release. It also does nothing for the
`bin/` CLI shim. Worth a spike if Q1 matters.

*Alternative C: require `godot-stagehand` on `PATH`.* No download code at all,
but install stays two steps and versions can skew. It defeats the point.

### D4. Names

- Plugin `godot-stagehand` and marketplace `godot-stagehand`, so the install
  id is `godot-stagehand@godot-stagehand`. That is redundant but conventional,
  and neither name hits a reserved prefix.
- MCP server key `stagehand`. Plugin MCP tools are named
  `mcp__plugin_<plugin>_<server>__<tool>`, giving
  `mcp__plugin_godot-stagehand_stagehand__godot_click`. A server key of
  `godot-stagehand` would make that longer for no gain.
- Skill invoked as `/godot-stagehand:stagehand`. Claude also loads it on its
  description, as today.

### D5. The skill moves; there is one canonical copy

Plugin skills must be `skills/<name>/SKILL.md`, and component paths cannot
escape the plugin root, so the file has to move rather than be referenced. It
moves to `integrations/claude-code/skills/stagehand/SKILL.md`. SKILL.md is
also the layout other agent clients read, so the README's "Agent skill" row
points there for non-Claude users too.

Tests and comments that name the old path move with it:
`internal/mcpserver/skill_tools_test.go`,
`release_assets_contract_test.go` (the skill is still not a release asset),
the `build-release.sh` comment, and `docs/tools.md`.
`TestSkillToolReferencesAreRegistered` then guards the shipped plugin skill
directly.

The skill gains a short "first-time setup" section that runs
`godot-stagehand setup .` through the `bin/` shim. Inside the plugin, skill
content can reference `${CLAUDE_PLUGIN_ROOT}` where a path is needed.

### D6. `userConfig` holds only `binary_path`

```json
"userConfig": {
  "binary_path": {
    "type": "file",
    "title": "Stagehand binary (optional)",
    "description": "Use this godot-stagehand binary instead of downloading the release that matches the plugin version. Leave empty for the default.",
    "default": ""
  }
}
```

It serves contributors running a local build, and air-gapped users.
Everything in `docs/configuration.md` (`STAGEHAND_CALL_TIMEOUT_MS`,
`STAGEHAND_MULTI`, and so on) stays as environment variables. Mirroring them
into `userConfig` would create a second configuration surface to keep in
sync. Note that the `bin/` shim never sees `binary_path`, because plugin
options are not exported to Bash-tool commands. The shim always uses the
cached download, and the doc says so.

### D7. A mod adds a status band and a game pane

[Mods](https://code.claude.com/docs/en/plugins/mods/overview) are plugin
code that runs inside Claude Code and can draw. v1 ships two features:

- **Status band** (`AbovePrompt` render site): one line while at least one
  instance is connected, such as `stagehand · default 127.0.0.1:26700 ·
  connected`, with a count when there are several. With nothing connected it
  returns `next(e)` and draws nothing. It always keeps other mods' band content
  by placing `await next(e)` inside its own `Box`.
- **`/stagehand-view` pane**: registered with `immediate: true`, so it opens
  while Claude is working. It shows the last frame Claude captured as an
  `Image`, the capture time, and the status. The `r` button refreshes the frame
  through `godot_screenshot`, and `s` re-reads `godot_status`. The Desktop app
  has no `Image` element, and `Image` caps PNGs at 2 MiB. In either case the
  pane shows the frame's size and says to ask Claude for a screenshot.

**Revised after the mod spike (owner decision, 2026-10-03).** The spike
(Q7, Q8) found two problems with the original data flow:

- Claude Code shows the model `godot_status`'s structured content instead of
  its text.
- A `$.mcp.call` made inside a turn goes through the permission rules.

The owner chose an observe-only band and guidance carried in the JSON. The
two paragraphs below are the revised design.

**Data flow.** The mod only observes. A `tool.call` hook passes every call on
unchanged, and for tools named `mcp__plugin_godot-stagehand_stagehand__*` it
reads the awaited result:

- the PNG from `godot_screenshot`;
- the instance list from `godot_status` and `godot_list_instances`;
- the new instance from `godot_launch` (JSON result) and from a successful
  `godot_connect`, taken from the call's own `instance_id`, `host` and `port`
  arguments, so no prose is parsed;
- the removed instance from `godot_disconnect`.

The hook never calls the server. Because `$.mcp.call` fires `tool.call` too,
a call there would re-enter the hook, and inside a turn it needs a permission
grant (Q7). Only the pane's buttons call the server, through `$.mcp.call`
with the server name `plugin:godot-stagehand:stagehand`. Buttons run outside
a turn. There are no timers, so an idle game costs nothing. The band can go
stale when a game dies between calls. It corrects itself at Claude's next
status or lifecycle call, or when the user presses `s` in the pane.

`godot_status` gains structured content next to its unchanged text, through
mcp-go's `NewToolResultStructured` (pinned v1.1.0, `mcp/utils.go`). Per
instance it carries id, state, host, port, pid, engine and Stagehand
versions. Claude Code gives the model `JSON.stringify(structuredContent)` in
place of the text (Q8), so the JSON also carries the text's guidance in the
same words:

- a top-level `hint` when nothing is connected;
- a top-level `note` about one server process per client;
- a per-instance `note` when reconnecting gave up.

Other MCP clients still read the text, which a Go test pins byte for byte.
The mod reads the JSON. Claude Code passes it as the result's text, so the
mod parses that text and ignores a result that is not JSON.

**Rules.**

- **Additive only.** Mods need Claude Code v2.1.287 or later. The mod also
  stops under `disableAllHooks`, `--safe-mode`, `allowManagedModsOnly`, and
  the `sec-default` guard that Team and Enterprise plans load, while the MCP
  server and the skill keep loading. Neither the skill nor the docs may depend
  on the mod, and the skill does not mention it.
- **Never changes a tool call.** Every `tool.call` hook returns `next(e)`. The
  mod never returns `{ deny }`, `{ result }`, or a `tool.check` decision.
  Guarding risky tools such as `godot_evaluate` is left to permission rules.
- **Smallest call surface.** The mods API calls come from an allowlist:
  `command.register`, `ui.open`, `ui.resolve`, `ui.invalidate`, and
  `mcp.call`. No `process`, `http`, `fs`, `store`, `env`, or `model`. A gate
  checks the list against `claude plugin validate --json`, so the `calls:`
  line a cautious user reads shows a mod that only draws and talks to its own
  server.
- **State lives in module variables.** Nothing needs to survive a restart,
  because the band rebuilds from the next stagehand call. Without `$.state`,
  the plugin needs no `types/index.d.ts`.
- **Command name `stagehand-view`.** A bare `/stagehand` risks colliding with
  a user's own `stagehand` skill or command, and `$.command.register` throws
  on a taken name. Registration runs last in `session.start`, inside
  `try`/`catch`, as the mods docs advise.

*Considered and deferred (owner call, 2026-10-03):*

- **Restyled `godot_*` tool rows.** Cosmetic, and it touches the most Claude
  Code UI.
- **A guard that holds risky calls.** It overlaps with permission rules.
- **Polling `godot_status` on a timer.** It would catch a game that dies
  between calls, but it runs in every session that has the plugin enabled.

## Versioning and release changes

Three new mirrors of `internal/version/version.go`:

| Mirror | Field |
| --- | --- |
| `integrations/claude-code/.claude-plugin/plugin.json` | `version` |
| `integrations/claude-code/bin/godot-stagehand` | `VERSION=` constant |
| `.claude-plugin/marketplace.json` | `plugins[0].source.ref` (`vX.Y.Z`) |

`scripts/set-version.sh` rewrites them. `go test ./internal/version/` and
`build-release.sh --verify-only` check them, the same way they check
`plugin.cfg` today (`docs/versioning.md`).

`release.yml` and `build-release.sh` also publish a `SHA256SUMS` asset next to
the four binaries. This is independent of the plugin, but the launcher's
verification depends on it, and it is worth having regardless.
`release_assets_contract_test.go` gains the asset, and the
`smoke-release` job verifies it.

`docs/release-checklist.md` gains: push the bump commit and tag atomically,
then after the release is published, install the plugin fresh and run one
tool call.

## Gates

- **Manifest validity:** `claude plugin validate --strict` on both the
  marketplace root and the plugin root. Whether CI can run it (it needs the
  Claude Code CLI installed in the job) is Q4. Failing that, a Go test parses
  both JSON files and checks every referenced path exists, and the strict
  validate stays a release-checklist step.
- **Launcher:** shellcheck-clean with `set -euo pipefail`. *Implemented as
  `set -eu` with no pipelines (2026-10-03): Ubuntu's `/bin/sh`, dash 0.5.12,
  rejects it (`dash -c 'set -o pipefail'` → `Illegal option -o pipefail`).*
  A Go test runs it
  against an `httptest` server through a test-only `STAGEHAND_RELEASE_BASE`
  override and a fake binary. It asserts: checksum mismatch is refused and
  leaves nothing behind; a second run does not download again; stdout is
  empty before exec; an unknown platform fails with a clear message;
  `STAGEHAND_BINARY` short-circuits.
- **Mod, tests first:** `claude plugin test integrations/claude-code`. The
  tests cover these cases:
  - the band draws nothing when no instance is connected, and renders the
    stubbed `godot_status` structure when one is;
  - the band keeps what later mods drew;
  - the pane draws an `Image` on `terminal`, and text on `desktop` or when the
    PNG is over 2 MiB;
  - Refresh calls `mcp.call` for `godot_screenshot`;
  - every `tool.call` result passes through identical;
  - the `tool.call` hook never calls the server (revised D7);
  - a refused `command.register` does not break the band.
- **Mod types and lint, the strict floor:** `tsc --noEmit` with `strict` and
  the full strict family (`noUncheckedIndexedAccess`,
  `exactOptionalPropertyTypes`, `noImplicitOverride`,
  `noFallthroughCasesInSwitch`, `noImplicitReturns`,
  `noPropertyAccessFromIndexSignature`). typescript-eslint
  `strict-type-checked` with `--max-warnings=0`. Then `claude plugin validate
  --strict --json` with the calls allowlist from D7. All of it runs from
  `tools/claude-mod/` with `npm ci`. `.github/dependabot.yml` gains an `npm`
  entry for that directory. Running in CI depends on Q4 and Q10.
- **`godot_status` structure:** a Go test asserts the structured content
  matches the instance list, and that the text output is byte-identical to
  today's.
- **Existing gates:** `go test ./...`, `go vet ./...`, the private-reference
  test (this doc and every plugin file are in its scope).

## Migration for existing users

- Remove any hand-written `godot-stagehand` MCP entry before installing the
  plugin. Otherwise both servers run and every tool appears twice under two
  names.
- Tool names change from `mcp__godot-stagehand__*` to
  `mcp__plugin_godot-stagehand_stagehand__*`. Agent `tools:` allowlists,
  permission rules, and hook matchers keyed on the old prefix need the new
  one. Hook matchers on the bare server key never fire for plugin servers.
- Recommend **project scope** in the README: enable the plugin in the Godot
  project's committed `.claude/settings.json`, not user-wide. Then its tools,
  skill description, and server process cost nothing in non-Godot sessions.
- The band and `/stagehand-view` need Claude Code v2.1.287 or later. Older
  versions still get the MCP server and the skill, pending Q9.

## Open questions

These are to verify during implementation, not to decide now. The owner
decided Q1 and Q3 on 2026-10-03.

- **Q1. Native Windows (decided).** The launcher is POSIX sh. On Linux,
  macOS, and WSL it works. Whether Claude Code on native Windows can start a
  plugin stdio server whose command is a shell script is unverified. If it
  cannot, v1 documents native Windows as manual-config only, and Alternative B
  becomes a follow-up ticket. Windows does not block v1. Separately, WSL
  sessions in the Claude Desktop app cannot load plugins at all (mods
  overview, "Where mods run"), so the docs list them as manual-config too. The
  CLI in a WSL terminal is unaffected.
- **Q2.** Does `${user_config.binary_path}` with an empty default substitute
  to an empty string, or does it refuse to start the server? If it refuses,
  use exec-form `args`, or have the launcher read
  `CLAUDE_PLUGIN_OPTION_BINARY_PATH`, if MCP servers receive it (the docs list
  it only for hooks).
  Verified 2026-10-03 (CLI 2.1.288, `claude -p "/spike-probe" --plugin-dir
  <spike>` with this manifest's `env` and `userConfig`): it substitutes to an
  empty string and the server starts. The server command logged
  `STAGEHAND_BINARY set=yes value=[]`, and no `CLAUDE_PLUGIN_OPTION_*`
  variables reach it.
- **Q3 (decided).** First start downloads about 13 MB inside the MCP startup
  timeout (`MCP_TIMEOUT`). On a slow link the first session's server may time
  out and the second session works. Measure it. Add a SessionStart prefetch
  settings hook through the same launcher only if the timeout reproduces. v1
  ships without one. The atomic `mv` already makes the race safe.
- **Q4.** Can CI install the Claude Code CLI to run `claude plugin validate
  --strict`? With the mod this is load-bearing: `claude plugin test` needs the
  CLI too. The mods docs say tests need no session, sign-in, or network.
  Confirm that an unauthenticated CI job can run both commands. If it cannot,
  the mod gates become a release-checklist step, and that is called out as an
  exception to the testing rule.
  Verified 2026-10-03, locally, not yet on a runner: `npm ci` gets the CLI
  from `@anthropic-ai/claude-code@2.1.288` (a devDependency of
  `tools/claude-mod`). With a fresh `CLAUDE_CONFIG_DIR` and no sign-in,
  `claude plugin validate --strict` passes on both roots, and `claude plugin
  test` on a spike mod printed `1 pass 0 fail`. One trap: in a config dir
  where an earlier session cached the mods rollout switch off, `claude plugin
  test` refuses ("hooks modules are turned off in this process: the rollout
  switch was saved off by an earlier session"). A fresh runner has no such
  cache.
- **Q5.** The existing skill frontmatter uses an `arguments:` list. Confirm
  `claude plugin validate` accepts it as-is in `SKILL.md`.
  Verified 2026-10-03: `claude plugin validate --strict integrations/claude-code`
  passes. A negative control (frontmatter with `bogus_key: [unclosed`) also
  passed, so validate does not check SKILL.md frontmatter. The skill does
  load, though: `claude -p "/godot-stagehand:stagehand explore" --plugin-dir
  integrations/claude-code` against a local fake Messages API sent the model
  the skill body, ending `ARGUMENTS: explore`, and listed
  `godot-stagehand:stagehand` among the skills.
- **Q6.** The launcher-path → data-dir derivation in D3 step 2 matches an
  observed install layout, not a documented contract. The XDG fallback keeps
  it working if that layout changes. Confirm both paths in the launcher test.
  Verified 2026-10-03: a local install has `plugins/cache/<marketplace>/<plugin>/<version>/`
  next to `plugins/data/<plugin>-<marketplace>/`. `TestLauncherDerivesDataDirFromPluginCacheLayout`
  and `TestLauncherFallsBackToXDGCache` (`claude_plugin_launcher_test.go`)
  pass for both paths. An MCP server launched from `--plugin-dir` gets
  `CLAUDE_PLUGIN_DATA=<config>/plugins/data/godot-stagehand-inline`.
- **Q7.** Does `$.mcp.call` reach the session's running `stagehand` server,
  which holds the Godot connections, or does it start a second process? A
  second process would always report "not connected". In that case the pane
  drops its buttons, and the band reads only results it observed in
  `tool.call`.
  Verified 2026-10-03 (CLI 2.1.288): it reaches the session's server. A spike
  mod launched the game with `$.mcp.call("plugin:godot-stagehand:stagehand",
  "godot_launch", …)`, and Claude's own `godot_screenshot` then returned the
  frame, with one server process logged. Three findings the design did not
  expect:
  1. The server name is `plugin:godot-stagehand:stagehand`. A bare
     `stagehand` fails: `no connected MCP tool "godot_status" on a server
     named "stagehand"`.
  2. `$.mcp.call` fires `tool.call`, including the calling mod's own hook. A
     refresh made from that hook would re-enter it.
  3. Inside a turn, `$.mcp.call` goes through the permission rules. In `-p`
     with the tool not allowed: `$.mcp.call(plugin:godot-stagehand:stagehand,
     godot_launch) refused: Claude requested permissions to use
     mcp__plugin_godot-stagehand_stagehand__godot_launch, but you haven't
     granted it yet`. The same call from a command, outside a turn, ran
     without a grant. What an interactive session shows instead is
     unverified.
- **Q8.** Does `tool.call` fire for plugin MCP tools? What does `next(e)`
  resolve to for an MCP result carrying image content? The pane needs the PNG
  bytes from it.
  Verified 2026-10-03 (CLI 2.1.288). A fresh config pointed at a local fake
  Messages API through `ANTHROPIC_BASE_URL`, which scripted Claude's
  `tool_use` blocks, so no subscription usage. `tool.call` fires for
  Claude's calls to `mcp__plugin_godot-stagehand_stagehand__*`. For
  `godot_screenshot`, `next(e)` resolves to `{ ref, result: [{ type: "image",
  source: { type: "base64", media_type: "image/png", data } }, { type: "text",
  text: "[Image: source: <session dir>/tool-results/….png]" }], text,
  isReadOnly: true }`. The PNG bytes are `result[0].source.data`. Claude Code
  also saves the frame to a file and gives its path, which an `Image` element
  can take instead of bytes.
  **Finding that conflicts with D7:** when a result carries
  `structuredContent`, Claude Code gives the model
  `JSON.stringify(structuredContent)` instead of the text content. The request
  the fake API received held `{"instances":[]}` as `godot_status`'s
  tool_result, not "Connection: not connected … Use godot_connect …", although
  the binary sends that prose on the wire as `content`. The same happens with
  an output schema declared, and `$.mcp.call` returns the JSON as its text
  block with no `structuredContent` field. So D7's "unchanged text" holds on
  the wire, but Claude in Claude Code no longer reads the prose. Owner
  decision 2026-10-03: put the guidance into the JSON too (D7, revised).
- **Q9.** On Claude Code older than v2.1.287, is a `hooks.json` that holds
  only `modules` ignored, or does it fail the whole plugin? If it fails, the
  plugin states a minimum Claude Code version.
  Verified 2026-10-03 on 2.1.284 only: the mod is skipped and the rest loads.
  The session printed `hooks module not loaded: hooks modules are not turned
  on for installed plugins in this process`, and the plugin's MCP server
  still started. 2.1.284's `claude plugin validate` passes the same plugin.
  Versions before 2.1.284 are unverified.
- **Q10.** Where does CI get the `claude-code` and `claude-code/testing` type
  declarations for `tsc`? Either the copy the installed CLI writes for its
  build (Q4), or a vendored copy pinned to a CLI version. The GitHub copy can
  lag the CLI.
  Verified 2026-10-03 (CLI 2.1.288): only a session that loads the plugin
  writes them. `claude -p "/<mod command>" --plugin-dir <dir>`, with no
  sign-in and no model call, wrote `.claude-plugin/types/` (`claude-code`,
  `claude-code-tools`, `claude-code-mcp`, a `tsconfig.json`, and a
  `.gitignore` containing `*`) plus a root `tsconfig.json` that extends it.
  `claude plugin validate` and `claude plugin test` write nothing. Decision:
  never commit them. They are generated, they ignore themselves, and
  `claude-code-mcp` lists whichever MCP servers the writing session had.
  The repo `.gitignore` covers the root `tsconfig.json`. CI generates them
  with that unauthenticated `-p` run before `tsc`. Not yet run in CI.
- **Q11.** How often does a real game's full-resolution PNG exceed the 2 MiB
  `Image` limit? The addon's capture is unscaled
  (`core/screenshot_capture.gd`). If it happens often, a follow-up adds a
  downscale parameter to the addon's screenshot. That is an addon change, so
  it is out of v1.
  Partly verified 2026-10-03: the `testdata/test_project` frame (1152×648) is
  16,872 base64 characters, about 12 KiB of PNG, far under 2 MiB. No real
  game was measured, so how often real games exceed the limit is still open.

## Implementation order (after sign-off)

1. `SHA256SUMS` release asset plus its contract test. Independent; can land
   first.
2. Plugin skeleton: `plugin.json`, the skill move and test path updates, and
   the version mirrors in `set-version.sh` and the version test.
3. Launcher plus its Go test and shellcheck.
4. `marketplace.json` plus the validate gate.
5. `godot_status` structured content plus its Go test. Independent of the
   plugin.
6. Mod spike: settle Q7 and Q8 with a throwaway `--plugin-dir` mod before
   writing the real one, because both can reshape D7.
7. Mod: `tools/claude-mod/` toolchain and the Dependabot `npm` entry, then the
   failing `register.test.ts`, then `register.ts`, then the calls-allowlist
   gate.
8. Docs: README install subsection (diff previewed before it lands),
   `docs/tools.md`, quickstart, and the release-checklist steps.
9. First tagged release that contains the plugin. Live install from GitHub
   into a fresh Godot project on every platform available. Then one
   `godot_launch` and one `godot_screenshot` through the plugin's tools, with
   the band showing the instance and `/stagehand-view` showing the frame.
