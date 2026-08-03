# Directory submission kit

Paste-ready submissions for the MCP-directory and Godot-directory listings named
in the [growth plan](growth-plan.md) (Phase 1). Everything here was verified
against the live target as of 2026-07-26 — repo existence, contribution rules,
and file locations were read directly (`gh`/`curl`), not guessed. Anything that
couldn't be verified is labelled below rather than assumed.

**Release status**: v0.3.1 is published with all four platform binaries; the
`releases/latest/download/...` links used below resolve correctly (checked via
`curl -I`, confirmed 302 to `releases/download/v0.3.1/...`).

**The positioning line every blurb below carries**: Stagehand automates a
*running* Godot game via MCP — its concrete edge over other runtime-capable
Godot tools is automated visual regression (screenshot diffing against saved
baselines, not just raw capture) and a standalone CI scenario runner (JUnit
output, exit codes, no MCP client required). See growth plan "Competitor
deep-dive: godot-mcp-enhanced" for the source-verified comparison this is
based on.

Standard one-line pitch (used verbatim or lightly trimmed to fit each
target's format):

> Godot Stagehand — MCP server and CLI for a *running* Godot game: click
> buttons, read node state, take screenshots, diff them against saved
> baselines, and run scenarios in CI with JUnit output and exit codes — no
> MCP client required. Playwright, but for game engines.

---

## 1. `modelcontextprotocol/servers` — **not currently submittable; the mechanism changed**

**Finding, not a guess**: I read `github.com/modelcontextprotocol/servers`'s
current `README.md` and `CONTRIBUTING.md` directly. The README no longer lists
third-party servers at all:

> If you are looking for a list of MCP servers, you can browse published
> servers on [the MCP Registry](https://registry.modelcontextprotocol.io/).

And `CONTRIBUTING.md` is explicit:

> The README no longer contains a list of third-party MCP servers — that list
> has been retired in favor of the MCP Server Registry... We don't accept:
> New server implementations.

So "PR to the README" — the ticket's original ask — is dead. The live
replacement is publishing to the **MCP Registry**
(<https://registry.modelcontextprotocol.io/>), via the `mcp-publisher` CLI and
a `server.json` manifest.

**Why this can't be done today**: the registry hosts only *metadata*; the
artifact itself must live in a supported package registry. I checked
`docs/modelcontextprotocol-io/package-types.mdx` in the registry repo — the
supported `registryType`s are `npm`, `pypi`, `nuget`, `cargo`, `oci`
(Docker/GHCR/Quay/etc.), and `mcpb`. Stagehand ships as raw platform binaries
attached to a GitHub Release (`godot-stagehand-linux-amd64`, etc.) — none of
npm/PyPI/NuGet/Cargo/OCI apply, and `mcpb` specifically requires a packaged
**`.mcpb` bundle** (a distinct zip+manifest format, not a bare binary) with a
`fileSha256` in `server.json`. Stagehand doesn't produce one.

**What it would take** (future work, out of scope for this doc-only kit):
someone packages an `.mcpb` bundle as an additional release asset (URL must
contain "mcp" — the repo name `godot-stagehand` already satisfies that), then:

```bash
mcp-publisher init      # scaffolds server.json in the repo
mcp-publisher login --github
mcp-publisher publish
```

`server.json` name would be `io.github.mrf/godot-stagehand` (GitHub auth forces
the `io.github.<username>/` prefix). Draft `server.json` for when that's ready:

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
  "name": "io.github.mrf/godot-stagehand",
  "title": "Godot Stagehand",
  "description": "Automates a running Godot game via MCP: click, read node state, screenshot + diff against baselines, performance asserts, plus a standalone CI scenario runner (JUnit, no MCP client needed).",
  "version": "0.3.1",
  "packages": [
    {
      "registryType": "mcpb",
      "identifier": "https://github.com/mrf/godot-stagehand/releases/download/v0.3.1/godot-stagehand.mcpb",
      "fileSha256": "<sha256 of the .mcpb once it exists>",
      "transport": { "type": "stdio" }
    }
  ]
}
```

**Recommendation**: skip this target until an `.mcpb` bundle exists, or
decide it's not worth the packaging effort given the other three targets below
already put Stagehand in front of MCP-client users.

---

## 2. Awesome-MCP-Servers lists

Three independent lists exist under this name; verified each one's current PR
policy directly.

### 2a. `punkpeye/awesome-mcp-servers` (91k★ — the de facto canonical one)

- **Location**: PR against `README.md`, `### 🎮 Gaming` section
  (`<a name="gaming"></a>`), currently ~50 entries, mostly Godot/Unity/Unreal
  editor-automation servers plus game-data servers.
- **Rules** (from `CONTRIBUTING.md`, read directly): one server per PR, one
  line per server, alphabetical order within the category, follow the
  existing legend (see below). **Automated-agent PRs get fast-tracked if the
  PR title ends in `🤖🤖🤖`** — explicit note in their CONTRIBUTING.md.
- **Legend glyphs that apply**: 🏎️ = Go codebase, 🏠 = Local Service (vs ☁️
  Cloud), 🍎🪟🐧 = macOS/Windows/Linux. No 🎖️ (that's for official
  first-party integrations, doesn't apply).
- **Entry to paste** (insert alphabetically by repo path — nearest correct
  spot in the current list is between `lodordev/mcp-romm` and
  `truetick-gg/mcp`; note the existing list is not perfectly sorted, don't
  let that block a correct alphabetical insert):

  ```markdown
  - [mrf/godot-stagehand](https://github.com/mrf/godot-stagehand) 🏎️ 🏠 🍎 🪟 🐧 - Automates a *running* Godot game via MCP: click, read node state, screenshots diffed against baselines, performance asserts, plus a standalone CI scenario runner (JUnit + exit codes, no MCP client needed). Playwright, but for game engines. MIT.
  ```

- **PR title**: `Add Godot Stagehand to Gaming 🤖🤖🤖` (the agent-PR emoji
  suffix is optional but fast-tracks review per their own docs — include it
  since this submission kit itself was agent-produced, or drop the emoji if
  Mark prefers to submit it as a human PR).
- **PR body**:
  > Adds Godot Stagehand, an MCP server + CLI that automates a *running*
  > Godot game (clicking, node inspection, screenshots, performance
  > assertions). Its edge over other runtime-capable Godot tools: automated
  > visual regression (screenshot diffing against baselines, not just raw
  > capture) and a standalone CI scenario runner with JUnit output and exit
  > codes, usable without an MCP client. MIT licensed, cross-platform
  > binaries, no Go toolchain required to use it.
  > Repo: https://github.com/mrf/godot-stagehand

### 2b. `appcypher/awesome-mcp-servers` (5.7k★)

- **Location**: PR against `README.md`, `## 🎮 Gaming` section
  (`<a name="gaming"></a>`), currently only 3 entries (all Unity).
- **Rules** (from `CONTRIBUTING.md`): one suggestion per PR, additions go at
  the bottom of the category, **alphabetical order required**, succinct
  description, check spelling/grammar and trailing whitespace.
- **Format note**: existing entries in this file use a
  `<img src="https://cdn.simpleicons.org/..." height="14"/>` brand icon before
  the link. This is cosmetic and not mentioned in CONTRIBUTING as required —
  included below for consistency; safe to drop the `<img>` tag if it doesn't
  render cleanly.
- **Entry to paste** (alphabetically after "UnityEngine" entries, i.e. at the
  bottom of the three existing Gaming entries — "Godot" < "Unity"
  alphabetically, so it actually belongs *first* in the category; move it to
  the top of the Gaming list):

  ```markdown
  - <img src="https://cdn.simpleicons.org/godotengine/478CBF" height="14"/> [Godot Stagehand](https://github.com/mrf/godot-stagehand) - Automates a *running* Godot game via MCP: click, read node state, screenshot + diff against baselines, assert performance, plus a standalone CI scenario runner (JUnit, no MCP client needed).
  ```

- **PR title**: `Add Godot Stagehand to Gaming`
- **PR body**:
  > Godot Stagehand automates a *running* Godot game via MCP — clicking,
  > node/property inspection, screenshots diffed against baselines,
  > performance assertions, input record/replay, plus a standalone CI
  > scenario runner (JUnit output, exit codes, no MCP client needed). MIT,
  > prebuilt binaries for Linux/macOS/Windows.

### 2c. `wong2/awesome-mcp-servers` (4.2k★) — **not a PR target**

Verified directly: the README's top line reads

> We do not accept PRs. Please submit your MCP on the website:
> https://mcpservers.org/submit

This is the same site as target 3d below (mcpservers.org) — submitting there
covers this list too. Don't file a duplicate PR against this repo.

---

## 3. Aggregator sites (mcp.so / Smithery / PulseMCP / mcpservers.org)

These are form/manifest-driven, not PR-driven, so "exact entry text" below
means the form-field answers rather than a markdown line. Two of the four
have concrete GitHub-verified mechanisms; two are form-only and their exact
field lists are marked unverified — their `/submit` pages are interactive
JS forms not reachable from this session's WebFetch allowlist or well-indexed
by search.

### 3a. mcp.so

- **Submit at**: https://mcp.so/submit — form-based ("Submit MCP Server or
  Client"), draft publishes on save. Only public GitHub-hosted MCP servers
  are accepted through this form (per the underlying open-source project,
  `chatmcp/mcp-directory`); non-open-source projects need a separate support
  path.
- **Fields** (name, description, features/capabilities, connection info) —
  confirmed these categories exist; **exact field-by-field wording and any
  length caps are unverified** — confirm on the live form before submitting.
- **Field answers to use**:
  - Name: `Godot Stagehand`
  - Repo URL: `https://github.com/mrf/godot-stagehand`
  - Description: the standard one-line pitch at the top of this doc.
  - Category: Gaming / Game Development (whatever the form's closest option
    is).

### 3b. Smithery (smithery.ai)

- **Not a listing form** — Smithery deploys *from* a connected GitHub repo,
  it doesn't take a submitted description. Flow (per Smithery's own
  publish docs): go to https://smithery.ai/new, connect
  `mrf/godot-stagehand`, and Smithery looks for a `smithery.yaml` (start
  command type `stdio`/`http`, a `configSchema`) plus a `Dockerfile` in the
  repo root. **Stagehand's repo has neither today** — this needs actual
  packaging work (a Dockerfile wrapping the Go binary, plus
  `smithery.yaml`), not just a form submission. Out of scope for this
  doc-only kit; flagging as a prerequisite rather than a paste-ready item.
- Smithery can auto-generate a PR adding these files if you start the
  connect flow — that PR would need human review before merging, same as
  any other code change, not a rubber-stamp submission.

### 3c. PulseMCP (pulsemcp.com)

- **Submit at**: https://www.pulsemcp.com/submit. PulseMCP also crawls
  GitHub and ingests the official MCP Registry automatically, so once/if
  target 1 above is ever completed, Stagehand may show up here without a
  manual submission.
- **Exact form fields and any description-length limit: unverified** — the
  submit page is a JS form not reachable via this session's tools. Confirm
  live before submitting; the standard one-line pitch at the top of this
  doc is a safe default to paste into whatever description field exists.

### 3d. mcpservers.org

- **Submit at**: https://mcpservers.org/submit — confirmed as the canonical
  replacement for `wong2/awesome-mcp-servers` PRs (see 2c above).
- **Exact form fields: unverified** for the same reason as PulseMCP above.
  Use the standard pitch; category should be Gaming/Game Development if
  offered.

---

## 4. `awesome-godot` — PR

- **Location**: `godotengine/awesome-godot`, file `README.md`, section
  `## Plugins and scripts` → `#### Godot 4` subsection. Verified by reading
  the file directly — this is where GdUnit4, GUT, and Vest (the existing
  testing-tool entries) already live, so it's the correct section for
  another testing tool, not a new category.
- **Rules** (from `CONTRIBUTING.md`, read directly):
  - Project must have a free/libre license — Stagehand is MIT, satisfies
    this.
  - "Must be useful in a project" — satisfied.
  - Follow existing style; **sort in alphabetical order**; categorize by
    newest compatible Godot version (Stagehand → Godot 4, minimum supported
    4.3 per [docs/compatibility.md](compatibility.md)).
  - No cryptocurrency/NFT integration — not applicable.
- **Alphabetical placement**: within `#### Godot 4`, entries run
  ...`Godot Shader Warmup` → `Godot Spin Button` → `Godot SQLite` →
  `Godot Torrent` → `Godot XR Tools`... "Godot Stagehand" sorts between
  `Godot SQLite` and `Godot Torrent`.
- **Entry to paste** (exact line, matching the file's existing one-line
  style — name link, en-dash-free single sentence, period):

  ```markdown
  - [Godot Stagehand](https://github.com/mrf/godot-stagehand) - External automation for a *running* Godot game — click buttons, read node state, take screenshots and diff them against baselines, assert performance — plus a standalone CI scenario runner with JUnit output, usable without an MCP client.
  ```

- **PR title**: `Add Godot Stagehand to Plugins and scripts`
- **PR body**:
  > Adds Godot Stagehand under Godot 4 plugins/scripts, alphabetically
  > between "Godot SQLite" and "Godot Torrent". It's a testing/automation
  > tool in the same vein as GdUnit4/GUT/Vest already listed here, but
  > drives the game at runtime from an external process (MCP server + CLI)
  > rather than running in-process. MIT licensed, minimum Godot 4.3.
  > Repo: https://github.com/mrf/godot-stagehand

---

## Summary — what's actually paste-ready vs. blocked

| Target | Status |
|---|---|
| `modelcontextprotocol/servers` | **Blocked** — README PR path retired; MCP Registry needs an `.mcpb` release asset that doesn't exist yet |
| `punkpeye/awesome-mcp-servers` | Paste-ready |
| `appcypher/awesome-mcp-servers` | Paste-ready |
| `wong2/awesome-mcp-servers` | N/A — redirects to mcpservers.org, no PR accepted |
| mcp.so | Paste-ready (form fields mostly known, some unverified) |
| Smithery | **Blocked** — needs a `Dockerfile` + `smithery.yaml` added to the repo first |
| PulseMCP | Form exists, exact fields unverified — use standard pitch |
| mcpservers.org | Form exists, exact fields unverified — use standard pitch |
| `awesome-godot` | Paste-ready |
