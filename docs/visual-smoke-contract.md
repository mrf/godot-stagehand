# Visual Smoke Contract

This document defines the boundary between Godot Stagehand and downstream game
repos for visual smoke testing. Read it when setting up a visual-smoke gate in
your game's CI, or when deciding where to encode a visual expectation.

## What Stagehand guarantees

Stagehand owns the **capture → compare → report** pipeline:

| Guarantee | Tool | Details |
|-----------|------|---------|
| Launch and connect to a Godot process | `godot_launch` | Blocks until the addon handshake succeeds |
| Capture a PNG screenshot of the running viewport | `godot_screenshot` | Validates non-empty, valid-dimension PNG before returning |
| Save a named baseline to disk | `godot_screenshot_save_baseline` | Writes `<name>.png` to the baseline dir; returns structured fields |
| Pixel-level diff against a saved baseline | `godot_screenshot_diff` | Two tolerance knobs (`threshold`, `pixel_sensitivity`); returns machine-readable `pass` boolean plus artifact paths on failure |
| Write diff artifacts on failure | — | `<name>-actual.png` + `<name>-diff.png` in the artifact dir |
| Surface diagnostics when capture fails | — | `viewport_image_empty`, dimension mismatches, and addon error strings are surfaced as structured errors, not silent blanks |

The end-to-end proof lives in
[docs/screenshot-pixel-proof.md](screenshot-pixel-proof.md). If that proof
passes, Stagehand's capture → transport → PNG path is sound. A blank screenshot
in your game is your scene's display setup, not Stagehand.

Full semantics (tolerance knobs, artifact paths, result schema) are in
[docs/visual-regression.md](visual-regression.md).

## What game repos own

Stagehand provides **no** game-specific visual expectations. Your repo owns:

- **Deterministic scene setup.** Stagehand cannot know which scene to load,
  which objects to spawn, or which animations to pause before a screenshot. Your
  harness must put the game in a known state before calling save/diff.
- **Baseline images.** The `.png` files in your baseline directory are golden
  files. Commit them to your game repo; treat them exactly like checked-in
  fixtures.
- **Tolerance choices.** Only you know whether a 2% diff is a regression or
  expected noise from a particle emitter. Pick `threshold` and
  `pixel_sensitivity` values that reflect your scene's behavior.
- **Semantic assertions.** Stagehand tells you "N pixels changed"; it does not
  know whether that means the HUD is broken or the animation frame advanced.
  That judgment belongs in your harness.
- **Selector scoping.** If your scene has animated elements, scope baselines to
  a stable node (`godot_screenshot_save_baseline(name="hud", selector="HUD")`)
  so unrelated motion does not trip the diff.

**Do not encode game-specific visual expectations in Stagehand.** Upstream
changes to Stagehand are about the capture/compare primitive, not about what
any particular game's UI looks like. A PR that embeds `my_game_main_menu.png`
or hardcodes color expectations for a specific game scene would be rejected.

## Headless support

**Headless Godot cannot produce real screenshots.** This is not a limitation of
Stagehand — it is a property of Godot's headless dummy rendering driver, which
renders nothing to the viewport.

| Launch mode | Visual smoke possible? |
|-------------|------------------------|
| `godot_launch(headless=false, expect_screenshots=true)` | Yes — use this for visual gates |
| `godot_launch(headless=true)` | No — `godot_launch` rejects `expect_screenshots=true` with `headless=true` at call time |
| Manual launch without `--headless` on a real display | Yes — connect with `godot_connect` |

For CI, you need a real display. Options:

- **WSLg** — Windows Subsystem for Linux's GUI support exports `DISPLAY` and
  `WAYLAND_DISPLAY`; this is the supported local path on WSL.
- **Xvfb** with a software rasterizer — provides a virtual framebuffer in
  headless CI environments (GitHub Actions, etc.).

A headless launch with `expect_screenshots=false` is fine for structural tests
(scene tree, properties, signals). Use that mode for CI checks that do not
require rendered pixels.

## Pinning a Stagehand version

Pin Stagehand to a specific commit or release tag before enabling visual smoke
gates. The baseline → diff workflow is stable once captured, but **the capture
pipeline itself** (PNG encoding, addon wire format, diff algorithm) can change
between releases. A Stagehand upgrade that changes pixel output would invalidate
your baselines and flip your gates.

Recommended approach:

```bash
# Pin via go install with a specific commit
go install github.com/mrf/godot-stagehand@<commit-sha>

# Or pin in a go.mod if used as a library dependency
require github.com/mrf/godot-stagehand v0.2.0
```

Document your pinned version in your game repo's CI setup comments so reviewers
know to update it deliberately.

## Minimal harness example

This pattern follows the [fixture/baseline/diff workflow](visual-regression.md)
and is the recommended starting point for a visual-smoke gate in your game repo.

### One-time: save baselines

Run this locally against a known-good build. Commit the resulting PNGs.

```
# Launch your game with screenshot support
godot_launch(
  scene="res://scenes/MainMenu.tscn",
  headless=false,
  expect_screenshots=true
)

# Put the scene in a deterministic state (your harness does this)
# e.g. godot_call_method(selector="AnimationPlayer", method="stop")
#      godot_set_property(selector="TimerLabel", property="text", value="0:00")

# Save a baseline for the full viewport
godot_screenshot_save_baseline(name="main_menu")

# Save a cropped baseline scoped to a stable node
godot_screenshot_save_baseline(name="hud", selector="HUD")
```

Baselines land in `stagehand-baselines/` (relative to the server's working
directory). Commit those PNGs to your game repo.

### Every CI run: diff against baselines

```
godot_launch(
  scene="res://scenes/MainMenu.tscn",
  headless=false,
  expect_screenshots=true
)

# Reproduce the same deterministic state used during baseline capture

# Strict diff — any pixel change fails
godot_screenshot_diff(name="main_menu", threshold=0.0, pixel_sensitivity=0.0)

# Lenient diff — tolerate minor animation noise in a small badge
godot_screenshot_diff(name="hud", selector="HUD", threshold=0.01, pixel_sensitivity=0.02)
```

A passing diff returns `pass: true`. A failing diff returns `pass: false` and
writes `stagehand-diffs/<name>-actual.png` + `stagehand-diffs/<name>-diff.png`
(relative to the server's working directory, same as baselines above) for
inspection.

This CWD-relative default is specific to the MCP server. The scenario runner
(`godot-stagehand run`) resolves its default diff dir against the scenario
file instead, unless `--out-dir` is given — see
[cli.md § Artifacts](cli.md#artifacts).

Branch on the machine-readable `pass` field rather than parsing the text report.
See [visual-regression.md § Machine-readable result fields](visual-regression.md#machine-readable-result-fields)
for the full result schema.

### When a diff fails

1. Inspect `*-actual.png` — is this a real regression or an intentional change?
2. If intentional, refresh the baseline: `godot_screenshot_save_baseline(name="main_menu")` and commit the new PNG.
3. If a regression, fix the code and re-run.

## Related docs

- [docs/visual-regression.md](visual-regression.md) — full baseline/diff semantics, tolerance knobs, artifact paths, result schema
- [docs/screenshot-pixel-proof.md](screenshot-pixel-proof.md) — canonical proof that capture returns real pixels
- [docs/windows-setup.md](windows-setup.md) — WSL/Windows display setup for headed launches
