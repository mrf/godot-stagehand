# Growth plan — godot-stagehand

Written 2026-07-25. Current state: public since 2026-04-25, 7 stars, 0 forks,
**0 releases**, no repo topics, no homepage URL, not listed in the Godot Asset
Library.

## The honest diagnosis

Nothing here is a messaging problem. The funnel is broken at step 1: a Godot dev
who finds the repo is told "build from source, requires Go 1.25+". Most Godot
devs don't have a Go toolchain and won't install one to try a testing tool. As
of the v0.3.1 release, the in-editor Setup wizard's "Download server binary"
button resolves all four `/releases/latest/download/<asset>` URLs and the
downloaded binary runs (verified 2026-07-26 via `gh`/`curl` and a live
`--version` run of the linux binary); the wizard's actual GUI click inside a
running editor is still unverified. Before this release, the single largest
discovery channel for Godot addons — the Asset Library — was explicitly
blocked on a release existing.

So: ship binaries first, then distribute. Marketing before that just sends
traffic to a wall.

## Phase 0 — unblock install (do before any promotion)

1. Bump version (`./scripts/set-version.sh X.Y.Z`), tag, push. `.github/workflows/release.yml`
   already exists — the tag is what fires it. *Remote op: Mark's to run.*
2. Verify the four platform assets land with the exact names the editor wizard
   resolves (`release_assets.gd`), and that the wizard's download path works
   end-to-end on a clean project.
3. Drop the README's "no binary releases are published yet" caveat and make the
   first Setup step "download from Releases", with build-from-source second.
4. Add repo topics: `godot`, `godot-addon`, `godot-engine`, `mcp`,
   `model-context-protocol`, `game-testing`, `test-automation`, `gdscript`,
   `playwright`. GitHub topic pages and search are real inbound.
   Set homepage URL to the docs quickstart.

## Phase 1 — the two catalog listings (highest leverage, one-time)

**Godot Asset Library.** Submitted 2026-07-25 against v0.3.0.
This is the highest-ROI single action available: it puts the addon in front of
every Godot dev browsing Tools, from inside the editor.

**MCP server directories.** Stagehand is one of very few game-engine MCP
servers, which is a genuinely differentiated slot in a crowded directory:
- `modelcontextprotocol/servers` community list (PR to the README)
- Awesome-MCP-Servers lists (several; PRs)
- mcp.so / Smithery / PulseMCP style aggregators (submission forms)
- `awesome-godot` (PR — the canonical Godot resource list)

Each is a one-time PR/form and keeps paying out. Paste-ready entries, verified
locations/rules, and PR titles/bodies for every target above:
[`docs/directory-submission-kit.md`](directory-submission-kit.md). That doc
also found the `modelcontextprotocol/servers` README PR path no longer exists
(retired in favor of the MCP Registry, which needs packaging Stagehand doesn't
currently produce) — treat that one target as blocked, not one-time-PR-ready.

## Phase 2 — the demo that does the selling

The product is inherently visual and nobody will grasp it from a tool table. The
asset that converts is a **60–90 second screen recording**: a real Godot game
running, an agent driving it, a screenshot diff catching a UI regression, exit
code 5 in CI. Put it at the top of the README as a GIF/MP4, and reuse it in every
post below. One asset, five placements.

Second-best: `examples/minimal-game/` already exists — make it a 5-minute
"clone this, run this, watch an agent play it" path and link it from the README's
first screen.

## Phase 3 — where the users actually are

Ranked by fit, not size:

1. **r/godot** — a *dev-log/show-and-tell* post, not an ad. "I made Playwright
   for Godot so Claude can playtest my game" + the video. This subreddit is the
   single largest concentration of the target user.
2. **Godot Forum / godotengine.org community** — Tools & Addons section.
3. **Godot Discord** (#showcase / #tools) — post once, then be present. Ongoing
   presence in the addon-dev channels converts better than a launch post.
4. **Hacker News** Show HN — the angle is "AI agents can now play-test games",
   not "Godot addon". HN cares about the agent/MCP story; Godot devs care about
   the testing story. Two different headlines, same tool.
5. **Bluesky / Mastodon gamedev + #godot** — where a lot of Godot devs went.
   Post the video, tag `#godot #gamedev #indiedev`.
6. **MCP/Claude communities** — the Anthropic Discord, r/ClaudeAI,
   MCP-focused newsletters. Novel-use-case angle: MCP beyond APIs and databases.
7. **itch.io devlogs / Godot newsletters** — *This Week in Godot* and similar
   round-ups actively look for new tools; a short pitch email is cheap.

Do NOT do all seven in one day. Ship the release + Asset Library, then post to
r/godot, watch what breaks for real users, fix it, then do HN a few weeks later
with a hardened tool. A Show HN against a tool that fails on someone's first
install burns the one shot you get.

## Phase 4 — content that compounds

Blog posts / dev logs that rank and get re-shared, in priority order:

- **"I let Claude playtest my game for a week — here's what it found."** Real
  bugs, real screenshots. This is the post that gets shared, because it's a
  story with results, not a feature list. Your own game is the obvious source.
- **"Visual regression testing for Godot"** — a genuinely underserved search
  term; `docs/visual-smoke-contract.md` is 80% of the post already.
- **"Adding a game-testing gate to your Godot CI"** — targets teams, who are the
  stickiest users. The exit-code contract is a strong, concrete hook.

## Positioning notes

- **Lead with runtime-vs-editor, everywhere.** Surveyed the Asset Library's MCP
  listings 2026-07-25: 12 assets, and by their descriptions all 12 are
  editor/authoring tools (create scenes, nodes, scripts from inside the editor).
  Stagehand is the only one aimed at a *running* game. That is the sharpest
  differentiator available and it has to be in the first sentence of every
  listing, blurb and post — a reader landing in that list will otherwise
  pattern-match Stagehand as the thirteenth editor MCP. Corollary: do not rename
  toward the crowd; "Godot Stagehand" standing apart from five variants of
  "Godot MCP <adjective>" is an asset. Landed in the README 2026-07-25.
- **The one-line pitch is already good**: "Playwright, but for game engines."
  It does the entire explanatory job for anyone who has done web testing. Lead
  with it everywhere; don't rewrite it.
- **Two distinct audiences, two front doors.** (a) AI-agent people who want
  Claude to drive a game — MCP angle, and (b) Godot devs who want automated
  testing at all — CLI/CI/scenario-runner angle. The CLI half is arguably the
  broader market and is currently buried below the MCP tool table. Consider a
  README split, or two landing sections.
- **Don't hide the honesty.** The README's frankness about limitations
  (headless can't screenshot, performance assertions are coarse, replay isn't
  deterministic) is a credibility asset with this audience. Keep it.

## What to measure

Stars are vanity; these aren't: release-asset download counts per platform,
Asset Library install count, issues opened by non-Mark accounts (the real signal
that someone got far enough to hit a wall), and forks. Ten users who file bugs
beat 500 stars.

Definitions, sources, and what a good/bad trend looks like for each:
[growth-metrics.md](growth-metrics.md). Take a monthly reading with
`./scripts/growth-metrics.sh` and paste its row into that doc's readings table.

## Immediate next three actions

1. Cut and push a real release so binaries exist. *(Mark — remote op)*
2. Submit the Asset Library listing. *(Mark — needs Godot account)*
3. Record the 60-second demo video.

Everything in Phases 3–4 depends on 1 and 2 being done first.

## Competitor deep-dive: godot-mcp-enhanced (2026-07-26)

The 87s.5 Asset Library survey read 12 listings' blurbs, flagged
`godot-mcp-enhanced` as the name most implying broader scope, and said the
runtime-vs-editor claim needed a source-level check before it goes further
into public copy (submission kit, 87s.6). Cloned
`github.com/wgt19861219/godot-mcp-enhanced` at depth 1 and read source
directly — not the README — against six questions.

**1. Running game or editor-only?** Both, deliberately. It ships a documented
"three-tier architecture": Headless CLI, Editor WebSocket, and a **Game
Bridge — "TCP to running game": E2E testing, runtime debugging, input
simulation, state verification** (`README.en.md:57-65`; Chinese original
`README.md:76-84`). This is not aspirational copy — the runtime bridge is a
real, separate GDScript autoload (`src/scripts/mcp_bridge.gd:1-14`, 1524
lines) distinct from the editor plugin (`addons/godot_mcp_server/`,
`plugin.cfg:4`: "AI Model Context Protocol bridge for **Godot Editor**").

**2. Transport/protocol.** The editor layer is a WebSocket server inside the
editor plugin (`addons/godot_mcp_server/websocket_server.gd`). The runtime
layer is a **TCP server with an NDJSON protocol**, default port 9081, run as
a project autoload (`mcp_bridge.gd:4-8`) — installed by copying the script
into the project and registering it under `[autoload]` in `project.godot`
(`src/tools/game-bridge.ts:545-586`, `game_bridge_install` action). Same
concept as Stagehand's addon-as-autoload install, different wire format
(NDJSON/TCP vs. our JSON-RPC 2.0/WebSocket).

**3. Tool surface overlap.** The merged `game` tool
(`src/tools/game-bridge.ts:370-402`) exposes `game_query` (`ping`,
`get_tree`, `find_nodes`, `get_node_properties`, `get_performance`,
`get_viewport_info`, `take_screenshot`), `game_write` (`set_node_property`,
`call_method`), `game_input` (`send_key`, `send_mouse_click`,
`send_mouse_move`, `send_text`, `send_touch`, `send_drag`), `game_wait`
(`wait_for_node`, `wait_for_property`), plus `monitor_*`/`watch_*` property
and signal timelines and `find_ui_elements`/`click_button`
(`game-bridge.ts:384-388,704-751`). Direct overlap with our
`find_nodes`/`click`/`get_tree`/`wait_for_node`/`wait_for_property`. No
`wait_for_signal` equivalent found in the tool surface (only `watch_start`
polling, not a blocking wait); no `evaluate`-equivalent arbitrary-expression
runtime eval on the *game* bridge (arbitrary GDScript execution,
`execute_gdscript`, is headless-only per the README's closed-loop diagram).

**4. Input simulation / screenshots / visual diffing.** Input simulation is
real and comparable to ours (mouse click/move, key, touch, drag, text —
`game-bridge.ts:429-430`). Screenshots are real but narrower: the `screenshot`
tool only does `capture` (headless, explicitly documented as experimental
with a blank-frame warning) and `analyze` (hands the PNG to the *client's*
vision capability as base64 — no built-in image comparison at all)
(`src/tools/screenshot.ts:17-48,94-121`). Their "frame-verify" system
archives captured frames to a `proof/<runId>/` bundle for review
(`src/tools/frame-verify/proof-bundle.ts:1-13`) — grepped that whole
directory for diff/baseline/compare logic and found none. **No automated
pixel-diff or baseline-comparison feature exists anywhere in the source.**
Our `screenshot_diff`/`screenshot_save_baseline` is a genuine, confirmed
differentiator against this competitor.

**5. CLI/CI story.** `package.json:16-19` exposes two binaries: the MCP
server itself and a `godot-mcp-dashboard` TUI — no third, standalone
scenario-runner binary. `src/cli/router.ts:8` lists exactly four subcommands:
`setup`, `doctor`, `init`, `dashboard` — none of them "run a test scenario
file and emit JUnit/exit code." A repo-wide grep for `junit`/`JUnit` returned
nothing. There is a `verify_delivery`/`dev_loop` closed-loop concept
(README) and an npm-side `score:gate` code-quality gate for *their own*
CI — but nothing resembling our scenario runner (declarative scenario file →
JUnit + exit code, usable from any CI without an MCP client in the loop).
This is a real, confirmed gap on their side.

**6. Activity, license, version support, install.** Very active: created
2026-03-21, pushed 2026-07-26 (today), 76 stars, weekly-ish tagged releases
through v0.24.0 (2026-07-25), per `gh repo view`. `LICENSE` is MIT text with
three copyright holders (own author, Coding-Solo/godot-mcp, AssetForge) —
GitHub's own detector classifies it as "Other," likely confused by the
multi-holder header, but the license body itself is standard MIT. Godot
support: README claims a "4.5–4.7 compat matrix," `README.en.md:165` says
"tested 4.6+." Install path for the runtime bridge is the
`game_bridge_install` tool call described in point 2 above, not a manual
Asset-Library-style addon drop.

### Verdict: the claim does NOT survive contact with the source

**"Drives your running game, not the Godot editor" is false as stated
against this competitor.** `godot-mcp-enhanced` has a real, working runtime
bridge (`mcp_bridge.gd`) with input simulation, node queries, and screenshot
capture against the *running game*, documented plainly as one of its three
tiers (`README.en.md:65`). The accurate differentiators against this
specific competitor are narrower than the blanket claim:

- We have automated visual regression (`screenshot_diff` +
  baselines); they only have raw capture + hand-off to client vision, no
  diffing at all.
- We have a standalone scenario runner with JUnit + exit codes usable from
  any CI without an MCP client; they don't.
- They cover far more ground *outside* runtime automation (33 tools / 199
  actions: scenes, scripts, animation, physics, particles, navigation, audio,
  3D asset generation, Blender modeling) — we are narrower and
  runtime-focused by design, not by capability gap.

A finding has been filed (see `.orchestrator-findings.jsonl`) to revise the
public-copy sites that carry the blanket claim.
