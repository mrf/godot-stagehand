# Visual regression testing for Godot

If you've searched for this, you already know the problem: Godot has no
built-in way to say "this scene should render the same way it did last week,"
and most testing advice for the engine stops at unit tests for your
GDScript logic. Nothing catches a shader change that silently breaks your
HUD, or a layout tweak that clips a label off-screen. That's visual
regression testing — comparing a rendered frame against a known-good
reference and failing loudly when they differ — and it's a gap in the Godot
tooling ecosystem.

[Godot Stagehand](https://github.com/mrf/godot-stagehand) is a WebSocket
addon plus a Go server (MCP + CLI) that drives a running Godot game
externally. One of its primitives is exactly this: capture a screenshot,
save it as a baseline, and diff future runs against it. This post covers how
that actually works, sourced from the project's own docs and tests rather
than a feature list — every command below is runnable against the CLI as
shipped.

## The two primitives

Everything comes down to two operations:

1. **Save a baseline** — capture the current rendered frame and write it to
   disk as the reference image.
2. **Diff** — capture a new frame and compare it, pixel by pixel, against
   the saved baseline.

Both exist as MCP tools (`godot_screenshot_save_baseline`,
`godot_screenshot_diff`) for agent-driven use, and as CLI flags
(`--baseline`, `--diff`) on `godot-stagehand screenshot` for CI. They're the
same underlying capture → compare pipeline either way.

One hard requirement up front: **screenshots need a real rendered window.**
Godot's `--headless` mode uses a dummy rendering driver that produces no
pixels at all — not a black frame, no frame. A screenshot attempt against a
headless instance surfaces this as a diagnostic (`viewport_image_empty`)
rather than returning a blank image silently. For CI, that means a real
display: WSLg on Windows/WSL, or Xvfb with a software rasterizer on Linux
runners.

## A runnable sequence: baseline, diff, exit code

Assuming you already have a Godot instance launched with the Stagehand
addon (see the project's CLI docs for `godot-stagehand connect`), the
one-time baseline capture and the repeated CI diff look like this:

```bash
export STAGEHAND_AUTH_TOKEN=<token this Godot session printed>

# One-time, against a known-good build: capture the golden reference
godot-stagehand screenshot --port 26788 --baseline main_menu

# ... time passes, code changes, someone touches the main menu shader ...

# Every CI run: diff the current frame against that reference
godot-stagehand screenshot --port 26788 --diff main_menu \
  --threshold 0.01 --pixel-sensitivity 0.02
echo $?
```

That `echo $?` is the part worth internalizing. Stagehand's CLI has a
stable exit-code contract, and the codes that matter here are:

| Code | Meaning |
|------|---------|
| 0 | Pass — the diff was within tolerance |
| 3 | Connection failure — never reached Godot; not a verdict on the pixels |
| 5 | **Assertion failed** — the game rendered, and it doesn't match. This is the real-regression code |

The 3-vs-5 split matters in a CI dashboard: exit 3 means your test
infrastructure is broken (wrong port, Godot didn't launch), exit 5 means the
game itself changed in a way your baseline didn't expect. Branching CI logic
on that distinction is the difference between "flaky test, retry" and "stop
the merge."

## Two independent tolerance knobs, not one

A naive pixel-diff either passes or fails on the slightest change, which is
useless once your scene has anti-aliasing, a blinking cursor, or a particle
effect. Stagehand splits tolerance into two orthogonal knobs so you can
tune "how different" separately from "how much":

- **`pixel_sensitivity`** (CLI: `--pixel-sensitivity`) — how far a single
  pixel's color can drift (per RGBA channel, normalized to `0.0`–`1.0`)
  before that pixel is even counted as different. `0.0` means any channel
  delta counts; `0.05` absorbs anti-aliasing and minor compression-style
  noise.
- **`threshold`** (CLI: `--threshold`) — once pixels are counted, what
  fraction of the total frame is allowed to differ before the diff fails.
  `0.0` fails on a single differing pixel; `0.01` tolerates up to 1% of the
  frame changing, which is roughly the size of a blinking caret or a small
  animated badge.

A diff **fails when `diff_ratio > threshold`**, where `diff_ratio` is the
count of flagged pixels divided by total pixels. Pick sensitivity for "how
different is different," and threshold for "how much of the frame gets a
pass."

Some concrete combinations:

```bash
# Pixel-perfect — good for a static settings panel
godot-stagehand screenshot --port 26788 --diff settings \
  --threshold 0.0 --pixel-sensitivity 0.0

# Absorb anti-aliasing drift, but no large region may change
godot-stagehand screenshot --port 26788 --diff hud \
  --threshold 0.0 --pixel-sensitivity 0.05

# Allow a small animated badge (up to 2% of the frame) to differ
godot-stagehand screenshot --port 26788 --diff main_menu \
  --threshold 0.02 --pixel-sensitivity 0.0
```

## Scoping a baseline to one node

A full-viewport diff is brittle if any part of the scene animates —
a spinning loading icon will fail every run even when the UI you actually
care about is fine. Both save and diff accept `--selector`, which crops the
capture to one node's bounding rect before comparing:

```bash
godot-stagehand screenshot --port 26788 --baseline hud --selector 'name:HUD'
godot-stagehand screenshot --port 26788 --diff hud --selector 'name:HUD'
```

Use the same selector for both save and diff — the captured dimensions have
to match or the comparison errors out rather than guessing.

## When a diff fails: the artifacts you get

A failing diff doesn't just print "no match." It writes two PNGs next to
your baselines (default `stagehand-diffs/`, configurable):

- `<name>-actual.png` — the frame that was actually captured this run.
- `<name>-diff.png` — a visualization where every differing pixel is
  rendered red and everything else is dimmed to a quarter of its brightness,
  so the change is visually obvious without diffing two images by eye.

If the change was intentional (you redesigned the panel on purpose), the
baseline is refreshed by re-running the save step with the same name — save
*is* the update, there's no separate refresh command. Review the
overwritten PNG in your diff before committing it, the same way you'd
review any other golden-file change.

## Wiring it into CI

The CLI path above is meant for poking at a live instance. For an actual CI
gate, Stagehand's scenario runner (`godot-stagehand run scenario.json`) lets
you describe launch, setup, and screenshot steps as data, with no live
terminal session involved. `save_baseline` and `screenshot_diff` are valid
scenario step actions, and a scenario is rejected at validation time —
before Godot is even launched — if it includes a screenshot step while
`headless` is `true`. That fails fast on a config mistake instead of burning
an engine startup to discover it.

```yaml
- name: Stagehand visual smoke
  run: |
    godot-stagehand run scenarios/menu-smoke.json \
      --out-dir stagehand-artifacts \
      --godot-bin "$GODOT_BIN"

- name: Upload Stagehand artifacts
  if: always()
  uses: actions/upload-artifact@v7
  with:
    name: stagehand-artifacts
    path: stagehand-artifacts/
```

The runner exits nonzero on any failed step, so no extra conditional logic
is needed to fail the build — a failed screenshot diff surfaces as exit
code 5, same as the CLI.

## What Stagehand does not do

Worth being explicit about, because it shapes how you'll actually use this:
Stagehand ships **no game-specific visual expectations**. It owns capture,
storage, comparison, and reporting — the primitive. Your project owns:

- Putting the scene in a deterministic state before capturing (stopping
  animations, setting known text) — Stagehand has no idea what "ready" means
  for your menu.
- The baseline PNGs themselves, committed to your repo as golden files.
- Choosing tolerance values that reflect your scene's actual behavior — only
  you know whether a 2% diff is a broken UI or an expected particle effect.
- Deciding whether "14,820 pixels changed" means something broke.

That boundary is deliberate. A tool that tried to guess "this looks like a
main menu, so it should look like other main menus" would be guessing wrong
constantly. A pixel-diff primitive with tunable tolerance, real exit codes,
and artifacts you can actually look at is a smaller claim, and it's one that
holds up.
