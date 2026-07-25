# Growth plan — godot-stagehand

Written 2026-07-25. Current state: public since 2026-04-25, 7 stars, 0 forks,
**0 releases**, no repo topics, no homepage URL, not listed in the Godot Asset
Library.

## The honest diagnosis

Nothing here is a messaging problem. The funnel is broken at step 1: a Godot dev
who finds the repo is told "build from source, requires Go 1.25+". Most Godot
devs don't have a Go toolchain and won't install one to try a testing tool. The
in-editor Setup wizard's "Download server binary" button hits
`/releases/latest/download/...` and 404s today
(`docs/asset-library-submission.md:29`). And the single largest discovery
channel for Godot addons — the Asset Library — is explicitly blocked on a
release existing.

So: ship binaries first, then distribute. Marketing before that just sends
traffic to a wall.

## Phase 0 — unblock install (do before any promotion)

1. Bump version (`./scripts/set-version.sh 0.3.0`), tag, push. `.github/workflows/release.yml`
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

**Godot Asset Library.** Everything is pre-written in
`docs/asset-library-submission.md` — field values, description copy, icon,
category. It needs Mark's Godot account and a pinned post-release commit hash.
This is the highest-ROI single action available: it puts the addon in front of
every Godot dev browsing Tools, from inside the editor.

**MCP server directories.** Stagehand is one of very few game-engine MCP
servers, which is a genuinely differentiated slot in a crowded directory:
- `modelcontextprotocol/servers` community list (PR to the README)
- Awesome-MCP-Servers lists (several; PRs)
- mcp.so / Smithery / PulseMCP style aggregators (submission forms)
- `awesome-godot` (PR — the canonical Godot resource list)

Each is a one-time PR/form and keeps paying out.

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

## Immediate next three actions

1. Cut and push a real release so binaries exist. *(Mark — remote op)*
2. Submit the Asset Library listing from `docs/asset-library-submission.md`.
   *(Mark — needs Godot account)*
3. Record the 60-second demo video.

Everything in Phases 3–4 depends on 1 and 2 being done first.
