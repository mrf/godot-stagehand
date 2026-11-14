# Backlog audit — 2026-11-14

Every open br ticket (56) checked against `main` at d87a3bf (v0.5.2) and ranked
against the goals in `docs/growth-plan.md`: a new user's first contact works,
then distribution, then hardening. Code claims were checked by reading the cited
lines; nothing was executed. Live checks (GitHub, Pages, Asset Library) were
plain GETs.

## Verdict

The backlog is legitimate. Every defect the external-review tickets describe is
still present at HEAD. Two tickets were finished but open, one is 90% finished,
and one real gap had no ticket. The ordering problem was that priority followed
the reviewer's severity (security first), so known first-contact bugs sat at P2
behind a nine-ticket P1 infrastructure epic.

## Changes applied in br

| Ticket | Change | Why |
|---|---|---|
| r0ri (report card) | closed | Landed c578e20. Pages URL serves the report (200), workflow green on main today, coverage exposed, no goreportcard refs. |
| 90vy (7ek out-of-scope commits) | closed | rk4e's close reason records Mark confirming 7b0b337 on 2026-07-25. Docs layout built on since. |
| 327t.17 wizard Test connection | P2 → P1 | `docs/quickstart.md:133` tells users to click it; the server rejects unauthenticated `ping`, so it can never pass. |
| 327t.22 input sim | P2 → P1 | Unknown action name returns `success: true`; `\n` does not submit. |
| 327t.23 selectors | P2 → P1 | No visibility check anywhere; `text=Back` picks a hidden menu's button and fails as "covered". |
| 327t.28 SKILL.md | P2 → P1 | What the Claude Code plugin's agent reads; omits tools the plugin hooks use. Cheap. |
| 327t.10 Go client | P2 → P3 | Needs a hostile or mismatched process on the local port. |
| 327t.21 addon param typing | P2 → P3, after .16/.20/.22 | Go frontends type-check first; rewrites 46 param reads across six files, so it must run last. |
| 7fgr interval capture | P2 → P3, needs-human | Three new public tools with no design sign-off; serves a private project, not adoption. |
| ez2g README badges | P2 → P3, needs-human | Landed 5298833; coverage badge then removed by the 08-03 README cut. Only Mark's call remains. |
| rq21 | needs-human, after .24 | It asks for a decision; half of it duplicates 327t.20 AC2. |
| y7sc, 58t, 8t7 | → P4 | Parked: low-risk symlink case and two upstream limitations. |
| 87s.25 (new) | filed P1 needs-human | Asset Library lists 0.4.1; latest is 0.5.2. See below. |

Dependencies added so same-file work cannot run in parallel: `.18→.17`
(setup_panel.gd), `.16→.22` (input_simulator.gd), `.21→.16/.20/.22`,
`y7sc→.15` (input_recorder.gd), `rq21→.24` (waiter.gd), `.30→.19`
(Scenario.Validate), and `87s.10→87s.23` (the ticket's own stated order, never
encoded). No cycles.

A scope guard was added to 327t.12: its AC2 must not undo 85fe, which
deliberately keeps teardown steps out of JUnit testcases.

## The gap: Asset Library is two minors behind

`godotengine.org/asset-library/api/asset?filter=stagehand&godot_version=4.3`
returns asset 5440 at `version_string` 0.4.1, modified 2026-09-01. Fifteen addon
commits separate that from v0.5.2, including the P0 safe-mode bypass fix. The
release checklist has no Asset Library step, which is how 0.5.0–0.5.2 shipped
without one. Filed as 87s.25.

## Left alone, with a recommendation

- **Host-compat (9 tickets, P1).** Mark set this order on 2026-10-14, so it
  stays. Two notes: the Godot 4.7.x install in argp.6 is not blocked by argp.1
  and can happen now; argp.10 (CI) waits on Source of Mana although the PR
  canary is Pixelorama-only, so depending on argp.7 alone would land the canary
  one host sooner.
- **gf08 feature picks.** Item 1 (error, log and crash capture) is the one that
  matters most for both front doors: today a scenario passes while the game
  logs script errors, and an agent cannot see them. Item 8 (diff masks,
  `--update-baselines`) deepens the one differentiator confirmed against
  godot-mcp-enhanced.
- **c3fg.** Valid, two lines. Fold into 327t.35, which should be the last
  docs-touching ticket to run.

## Waiting on Mark

1. 87s.7 demo video — gates every community post.
2. 87s.25 Asset Library edit.
3. argp.6 clones and Godot 4.7.x.
4. 327t.37 trust-model decisions; 327t.36 action SHAs.
5. ez2g: Go coverage badge back in the README, or close.
6. 7fgr and rq21: confirm or drop.
7. gf08: pick items.

## Per-ticket verification

`V` verified by reading at d87a3bf. `H` credible but the ticket's own mechanism
is an untested hypothesis. `RC` partly reviewer-cited, not re-read.

| Ticket | P | State | Note |
|---|---|---|---|
| 87s.7 demo video | 1 | valid | Mark's. |
| 87s.10 r/godot | 1 | valid | Blocked on .7 and now .23. |
| 87s.23 GitHub readiness | 1 | V | No ISSUE_TEMPLATE, CONTRIBUTING or SECURITY.md. GitHub reports 0 issues. First worker ticket. |
| 87s.25 Asset Library | 1 | new | — |
| argp.1–.4, .6, .7, .9, .10 | 1 | valid | `internal/hostcompat/` and the manifest exist; no checkout script, no `.hostcompat/`, only Godot 4.6.2 installed. |
| 327t.17 | 1 | V | — |
| 327t.22 | 1 | V (item 3 RC) | — |
| 327t.23 | 1 | V (item 4 RC) | — |
| 327t.28 | 1 | V | — |
| 327t.12 JUnit | 2 | V | See scope guard. |
| 327t.27 exit codes | 2 | V | Size mismatch is a plain error, classified internal rather than a failed diff; an artifact error is returned before the scenario's own exit code. |
| 327t.15 recording | 2 | H | Recorder uses `_input` and `parse_input_event`; simulator uses `push_input(ev, true)`. |
| 327t.16 touch/mouse | 2 | H | — |
| 327t.18 wizard download | 2 | V | Status check exists, as the ticket says. |
| 327t.19 baseline_dir | 2 | V | No containment; docs lack the trust statement. |
| 327t.20 blocklist | 2 | V | Both lists match each other and both miss the same methods. |
| 327t.24 waits | 2 | V | `ci.visible`; `arg == null: break`. |
| 327t.25 launch | 2 | V | `instances.remove` precedes the launch; perf args unbounded. |
| 327t.26 setup | 2 | V (RC in part) | `strings.Contains` match; plain `os.WriteFile`. |
| 327t.29 param parity | 2 | V | gwpop requires `input_path`; MCP uses `recording_path`, `include_properties`. |
| 327t.37 decisions | 2 | valid | Mark's. |
| 87s.11, .12, .14 | 2 | valid | Correctly behind .10. |
| argp.11 | 2 | valid | — |
| 327t.10, .21 | 3 | V | Demoted above. |
| 327t.30 | 3 | V (RC in part) | Teardown not checked for screenshots under headless. |
| 327t.31 floats | 3 | V | `left == right`. |
| 327t.32 import stamp | 3 | V | — |
| 327t.33 WSL/quoting | 3 | V | Distro segment discarded. |
| 327t.34 CI hygiene | 3 | V | — |
| 327t.35 docs | 3 | RC | Run last. |
| 327t.36 supply chain | 3 | V | Tag pins only. Mark's. |
| ar4b, wpq1 | 3 | V | — |
| rq21, 7fgr, ez2g, gf08, c3fg, 87s.13 | 3 | valid | See above. |
| y7sc, 58t, 8t7 | 4 | valid | GdUnit4 still 6.1.3. |

## Not in br, noted in passing

- Dependabot PRs #27 and #28 are open (#28 is mcp-go 1.3.0 → 1.5.0).
- `docs/growth-plan.md` still opens with "0 releases".
- The 87s epic description is stale; a comment on it gives the current state.

`.beads/issues.jsonl` and this file are uncommitted.
