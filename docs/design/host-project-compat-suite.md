# Host-project compatibility suite

**Status:** design, awaiting review. Nothing here is implemented beyond the
manifest (`testdata/hostcompat/manifest.json`) and its validator
(`internal/hostcompat/`). No CI workflow exists yet; no third-party repository
has been cloned.

**Issue:** `godot-stagehand-host-project-compat-suite-9py8`

## Why

Stagehand is a tool. Its correctness is defined at the boundary with code we do
not control, and today the only integration surface is
`testdata/test_project/` — a fixture we wrote, which therefore shares our
assumptions. It has no CLI parser of its own, no modal dialogs and no content
scaling, so it cannot catch the class of bug that actually reaches users.

One afternoon against one real project (Pixelorama, `87s.18`) produced three:

| Finding | What broke |
| --- | --- |
| `87s.20` | The host parses its own argv and rejects `--stagehand`, so the documented activation path fails outright |
| `87s.21` | Synthesized input never reaches embedded `Window` subwindows |
| `87s.18` | `/root` had `content_scale_factor = 1.5`; the addon had no content-scale handling |

The Go suite was green throughout all three. That is the gap this suite closes,
and it is also the strongest available answer to the 1.0 readiness question: it
converts "nobody outside this repo has ever run it" into a standing, checkable
claim.

## Shape: fixtures and a job, not a framework

The runner already exists. `internal/scenario` gives us declarative
launch/act/assert steps, JSON and JUnit reporters, an RPC trace, artifact
capture, and typed exit codes; `.github/workflows/ci.yml`'s `scenario-smoke`
job already builds the binary, installs a cached Godot, and runs a scenario
against it. `scripts/ci-install-godot.sh` is already parameterised by
`GODOT_VERSION` and caches per version, so a multi-version matrix needs no new
installer.

**The host-compat suite is therefore: a manifest, one scenario file per project,
a checkout script, and one CI job.** Building a parallel test system would be
the main way this effort fails — a second framework is a second thing to keep
green, and it would drift from the one users actually run.

Three additions are genuinely needed, and they are small:

1. **A pointer from a checked-in scenario to a checkout outside the repo.**
   `target.project_path` resolves relative to the scenario file and has no
   variable expansion (`internal/scenario/scenario.go:57`). Recommended: add
   `run --project-path DIR` to override `target.project_path`, symmetric with
   the existing `--godot-bin` and `--baseline-dir` overrides
   (`internal/cli/run.go:23-31`). It keeps the checkout location a CI concern
   instead of a checked-in path. *Fallback with zero code:* fix the checkout
   directory at `<repo>/.hostcompat/checkouts/<id>` (gitignored) and write
   `"project_path": "../../../.hostcompat/checkouts/<id>"` into each scenario.
   That works today but silently breaks the moment anyone caches elsewhere.

2. **A checkout script**, `scripts/hostcompat-checkout.sh <id>`: clone with
   `--filter=blob:none`, check out the pinned SHA, verify `git rev-parse HEAD`
   matches the manifest, then run `godot-stagehand setup` (`internal/setup`)
   against `<checkout>/<project_subdir>`. Installing via the shipped `setup`
   command rather than a bespoke copy is deliberate — the install path is one
   of the things under test.

3. **The manifest validator**, `internal/hostcompat` — implemented in this
   branch, described below.

### Layout

```
testdata/hostcompat/
  manifest.json              # the pinned candidate set (checked in)
  <id>/scenario.json         # one host-compat scenario per project
.hostcompat/checkouts/<id>/  # clone destination, gitignored
```

## Pinning

**Every project is pinned to an immutable commit SHA. Nothing tracks a
third-party branch.** Their refactor becoming our red build is not a signal —
it is noise, and a suite that is red for reasons outside our control is one
people learn to mute and then delete. A pin bump is a deliberate, reviewed
change, and *a bump that breaks us is the signal this suite exists to produce.*

Each entry carries both `ref` (the human-readable tag the pin was taken from —
documentation, and a hint for whoever bumps it) and `commit` (40-hex, the
authority, because a tag can be moved). The checkout script verifies the SHA
after checkout rather than trusting the ref.

### The enable gate

Two rules are enforced by `Manifest.Validate` rather than by convention, because
convention is what erodes:

- **A project may not be `enabled` until its pin resolves to a commit SHA, it
  has a scenario fixture, and every claim about it is `verified` with a named
  source.** A `verified` claim without a `source` is a validation error.
- **An enabled project may not be pinned to a branch-shaped ref** (`main`,
  `master`, `HEAD`, …).

Unresolved candidates stay in the manifest, disabled. That is the point: the
outstanding verification work is visible and reviewable in a checked-in file
instead of living in someone's head. **Every project in the manifest today is
disabled**, because none of them has been cloned — see "Candidate set" below.

The four claims are `language` (GDScript, not Mono/C# — a C# project needs a
different engine build entirely), `godot_version` (read from the project's own
`project.godot`, not from a README), `license`, and `axes` (confirmed in the
project's files, not inferred from what the project appears to be).

## Assertion surface — deliberately narrow

The suite asserts that **Stagehand** can install, launch, connect, enumerate the
tree, drive input and capture a frame. It **never** asserts that the host
application behaves correctly. Their bugs are not ours.

A broad assertion surface is the main way a suite like this rots, so the surface
is a closed allowlist enforced by `hostcompat.ValidateScenario`, not a guideline:

| In surface | Why |
| --- | --- |
| `ping` | the connection came up and the addon answered |
| `tree`, `find`, `wait_for_node` | we can enumerate somebody else's scene |
| `assert_node_count` (`greater_than` / `gte` only) | "Stagehand can see at least one node of this kind" |
| `click`, `press_key`, `press_action`, `type_text`, `mouse_move`, `touch` | input reaches the game |
| `screenshot` | we can capture a frame of a real app |
| `sleep`, `get_performance` | pacing and engine counters — not host state |

| Rejected | Why |
| --- | --- |
| `assert_property` | reads host state; their refactor turns us red |
| `set_property` | mutates host state |
| `evaluate`, `call_method` | runs host code we did not write |
| `screenshot_diff`, `save_baseline` | pins the host's pixels; their UI redesign turns us red |
| `assert_node_count` with `equals` / `less_than` / `not_equals` | pins the host's layout — host behaviour by another name |
| `target.allow_unsafe` | nothing in the surface needs it |
| `target.mode: connect` | would attach to whatever game holds the port: a race in CI, someone else's game locally |

### The cost of drawing the line here — stated plainly

**The suite proves an input event was *delivered*, not that the host *reacted*
to it.** That is a real weakness and it is worth being honest about: `87s.21`
(input never reaching embedded subwindows) is exactly the kind of bug an
effect-level assertion would catch and a delivery-level one might not — the
addon reported `success: true` while the click went nowhere.

The mitigation is not to widen the surface. It is that every host-compat finding
gets a matching regression fixture in `testdata/test_project`, where asserting on
behaviour is legitimate because the behaviour is ours — the pattern
`internal/launch/contentscale_click_test.go` already follows. The host suite's
job is to *find* the class of bug; our own fixtures prove it stays fixed.

Two partial compensations are in surface and worth using: a `find`/`tree` call
after input still resolving proves the app did not crash, and
`assert_node_count class:Window greater_than 0` proves Stagehand can *see*
subwindow nodes — a claim about our enumeration, not their layout.

## Display: xvfb is required

Screenshots need a rendered frame, and headless Godot uses a dummy display
driver. The existing `scenario-smoke` job sidesteps this by being deliberately
screenshot-free; the host suite cannot, because "can Stagehand screenshot a real
third-party app" is one of the six things it exists to check.

So: `xvfb-run -a ./godot-stagehand run …`, with `headless: false` in host-compat
scenarios.

This matters beyond screenshots. Embedded-`Window` behaviour and content-scale
geometry can differ between the dummy driver and a real display, and those are
two of our six stress axes. (The content-scale click bug happened to be
reproducible headless — `internal/launch/contentscale_click_test.go` — because
headless Godot inherently runs with `window.size != content_scale_size`. That is
a lucky property of that one bug, not a general rule.)

## Cadence — recommendation

**Recommended: nightly full matrix + `workflow_dispatch` + a single-project
canary on PRs that touch the code this suite covers.**

Cost drives the choice. Each project costs a clone, a Godot download (cached), a
first import of an unfamiliar asset tree — minutes for a large project — plus the
run itself. Six projects across four engine versions is not a per-PR gate.

- **Nightly, full matrix.** Catches everything, and a break at most a day after
  the commit that caused it.
- **`workflow_dispatch`.** So a release candidate can be checked on demand
  without waiting for the nightly, and so a pin bump can be validated in its own
  PR.
- **PR canary — Pixelorama only, path-filtered** to `addons/stagehand/**`,
  `internal/launch/**`, `internal/godotconn/**`, `internal/setup/**`. Every bug
  this suite has found so far lived in one of those. This is one project on one
  engine version, so the cost is roughly the existing `scenario-smoke` job plus a
  clone.

**The tradeoff:** nightly means a break can be up to ~24h and several merges old,
so bisecting costs more than a per-PR gate would. The path-filtered canary buys
back most of that for the paths that historically break, at ~1/6 the cost of the
full matrix. If the canary proves cheap in practice, widening it is a one-line
change; starting wide and narrowing after people learn to ignore a slow gate is
not recoverable.

**Failure policy:** the nightly job must not be able to fail for reasons outside
our control. A clone or network failure is `infrastructure`, reported and
retried, and must never be reported as a compatibility failure — that
distinction is what keeps the signal trustworthy.

## Caching

Immutable pins make cache keys trivial and correct:

| Cache | Key | Notes |
| --- | --- | --- |
| Checkout | `hostcompat-src-<id>-<commit>` | a pin bump busts it automatically; nothing else can |
| Godot binary | `godot-<version>-linux` | already exists in `scenario-smoke` |
| Import cache (`.godot/`) | `hostcompat-import-<id>-<commit>-<godot_version>-<addon_hash>` | the big win — first import dominates |

The addon hash must be part of the import key: installing our addon adds files
to the project, so a changed addon changes the import set. Getting that key
wrong produces stale-import failures that look exactly like real compatibility
failures, which is the worst available outcome for this suite's credibility.

## Candidate set

Selection is by stress axis, not popularity. The six axes are a closed enum in
`internal/hostcompat`; `MissingAxes()` reports gaps and a test fails if any axis
is uncovered.

| Project | Godot | Axes | Verification |
| --- | --- | --- | --- |
| Pixelorama | 4.6.3 | own-cli-parser, embedded-subwindows, content-scale, large-project | language + version **verified** (`87s.18`, first-hand); licence and pin unverified |
| Material Maker | 4.3 | own-cli-parser, embedded-subwindows | all unverified |
| GodSVG | 4.5 | embedded-subwindows, content-scale | all unverified |
| Tanks of Freedom II | 4.2 | heavy-autoloads | all unverified; only candidate with an independently confirmed release tag (`1.0.7`) |
| OpenGamepadUI | 4.3 † | custom-input | all unverified; GPL-3.0, heaviest build |
| Source of Mana | 4.3 † | heavy-autoloads, large-project | all unverified |

† **Placeholder.** Search results said only "Godot 4" with no minor. Both were
set to a minor already present in the manifest so a guess cannot inflate the
claimed version spread. Read `config/features` from `project.godot` and correct
these before enabling.

Per-project sources, caveats and open questions are in the manifest's `claims`
and `notes` fields rather than duplicated here.

### Method and its limits

Candidate research was **WebSearch snippets only** — no repository was cloned or
fetched, per this ticket's scope. Snippets are second-hand. **Nothing about any
`project.godot` was verified for any project**, including every stretch mode,
autoload count, and subwindow usage. Most axis assignments are therefore
*hypotheses about where a project will stress us*, and the first implementation
step is to check them. The manifest's `claims` block records exactly this, per
project, with what would settle each one.

### Known gaps

- **No candidate on Godot 4.0 or 4.1.** Real gap. The likeliest fix is pinning an
  older tag of a project already listed, but which engine version each old tag
  targeted is unverified and must be checked before relying on it.
- **`content-scale` has no verified candidate.** Both assignments (Pixelorama,
  GodSVG) rest on UI-scale settings, and only Pixelorama's is backed by a
  first-hand observation (`content_scale_factor = 1.5`, `87s.18`). This is the
  weakest axis and should be confirmed by reading a `project.godot` early.
- **The `--` convention caveat.** Pixelorama and Material Maker both document a
  `--` user-args convention, which suggests they read
  `get_cmdline_user_args()` rather than raw argv — in which case an unknown
  `--stagehand` before `--` may be swallowed by the engine rather than rejected
  by the app. `87s.20` observed a real rejection, so the failure is real; the
  *mechanism* is unverified. Since `internal/launch/launch.go:205-206` places
  `--stagehand` after `--` and also sets `STAGEHAND_ENABLED=1`, establishing
  which path actually fails is a prerequisite for asserting anything about it.

### Rejected

- **Thrive** — C# on Godot .NET, plus Git LFS. Fails the GDScript requirement.
- **Lorien** — its only release tag targets Godot 3.5.3; the Godot 4 work lives
  on a branch, so it can only be pinned to a floating SHA with no release
  behind it.
- **Turbo Fat** — no evidence of a Godot 4 port.

**Reserve:** *Bosca Ceoil Blue* (MIT, GDScript, 4.3, tag `3.0-beta2` — the
best-verified candidate after Pixelorama) covers no axis the set does not
already have, and needs a GDExtension binary that is not in its repository. Worth
promoting if one of the six proves unworkable.

## Proposed epic split

Filed as one ticket to avoid sprawl before the shape was known. With the shape
settled, this splits along a dependency chain, not per-project — the per-project
children only become independent after (2).

1. **Resolve the pins.** Clone each candidate once; record the SHA; read
   `project.godot` for the real engine version, stretch mode and autoload count;
   read `LICENSE`; confirm GDScript. Update the manifest's `claims` and `axes` to
   match what is actually there. *Requires Mark — cloning is a remote git
   operation.* Blocks everything else. Expected output includes some axis
   reassignments and possibly a candidate swap.
2. **Plumbing.** `run --project-path`, `scripts/hostcompat-checkout.sh`, and a
   `.gitignore` entry for `.hostcompat/`. Small and mechanical; can start in
   parallel with (1).
3. **One child per project** — write `testdata/hostcompat/<id>/scenario.json`,
   run it locally, enable the entry. Independent of each other once (1) and (2)
   land. Each child's definition of done is a green local run plus a verified,
   enabled manifest entry. Order by axis value: Pixelorama, then OpenGamepadUI
   (custom-input, the least-covered axis), then the rest.
4. **The CI workflow.** Nightly matrix + `workflow_dispatch` + the path-filtered
   PR canary, xvfb, the three caches, and the infrastructure-vs-compatibility
   failure split. Needs at least two children from (3) to be meaningful.
5. **Regression-fixture backfill.** For every bug the suite finds, a fixture in
   `testdata/test_project` that proves it stays fixed. This is what makes the
   narrow assertion surface affordable, so it is a standing child, not a
   one-off.
6. **Fill the 4.0/4.1 gap.** Deliberately last: it may turn out to be
   unfillable with a qualifying project, and that answer is worth writing down
   rather than blocking on.

## Open questions for review

1. **`run --project-path` vs. the fixed-checkout-directory convention.** The
   flag is recommended; the convention needs no code. Worth a decision before
   (2).
2. **Is the delivery-vs-effect line drawn in the right place?** It is the single
   most consequential decision here, and it deliberately gives up the ability to
   catch an `87s.21`-shaped bug directly.
3. **Six projects, or fewer to start?** Nightly cost scales linearly, and the
   axes are covered by four of them (Pixelorama, GodSVG, Tanks of Freedom II,
   OpenGamepadUI). Starting at four and growing on evidence is defensible.
4. **OpenGamepadUI's build weight.** It is the only real candidate for the
   custom-input axis and the most expensive to build. If it is prohibitive, the
   axis needs a different owner rather than being quietly dropped.
