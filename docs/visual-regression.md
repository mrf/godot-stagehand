# Visual Regression: Baselines & Diffs

`godot_screenshot_save_baseline` and `godot_screenshot_diff` are generic visual
primitives. They make no assumptions about any particular game — you capture a
reference frame, then later assert the rendered output still matches it within a
tolerance you control. This document defines the storage, naming, refresh, and
tolerance semantics so downstream games and agents can rely on stable behavior.

> Visual tools need a **visible rendered window**. Launch with
> `godot_launch(headless=false, expect_screenshots=true)`. Headless launches
> render nothing and screenshots will be empty. See
> [`windows-setup.md`](windows-setup.md) for the WSL/Windows host.

## The workflow

1. **Save a baseline** once the scene looks correct:
   `godot_screenshot_save_baseline(name="main_menu")`
2. **Diff** on later runs to catch regressions:
   `godot_screenshot_diff(name="main_menu", threshold=0.0)`

A diff **passes** when `diff_ratio <= threshold` and **fails** otherwise. A
failing diff returns an MCP error result *and* writes artifacts you can inspect.

## Baseline storage & naming

- Baselines are PNGs written to the server's **baseline directory**, default
  `stagehand-baselines/` (relative to the server's working directory).
- The file is named **`<name>.png`** — the `name` argument is used verbatim as
  the filename stem, so it is validated against an allowlist before anything is
  written.

### Name allowlist

A baseline name must be:

- **ASCII letters, digits, `_`, `-` and `.`** — nothing else;
- **starting and ending with a letter or digit**;
- **free of any `..` sequence**;
- **at most 128 characters**;
- not a Windows reserved device name (`CON`, `NUL`, `COM1`, …).

Valid: `main_menu`, `hud_full`, `inventory_open`, `main-menu`, `menu.1080p`.

Rejected: `../escape`, `sub/menu`, `sub\menu`, `/tmp/x`, `C:\x`, `.hidden`,
`menu.`, `main menu`, `menü`, anything containing a control character. Path
separators (both platforms'), absolute and drive-relative paths, and dot
segments all fall outside the allowlist, so a name can never name a file
outside the baseline or artifact directory — the resolved path is additionally
proven to be a direct child of that directory before any write. This matters
because scenario files are data and may arrive from a pull request.

The same allowlist applies to the CLI/scenario `save_baseline` and
`screenshot_diff` steps, where it is enforced at scenario-parse time.
- One name = one baseline. There is no implicit per-resolution or per-platform
  namespacing — if you need those axes, encode them in the name
  (`main_menu_1080p`, `main_menu_linux`).

Commit baselines to your game's repo so diffs are reproducible across machines
and CI. Treat them like golden files.

## Refreshing / updating a baseline

When a visual change is **intentional**, refresh the baseline by re-running save
with the same name — it **overwrites** the existing file:

```
godot_screenshot_save_baseline(name="main_menu")
```

There is no separate "update" verb; save *is* the update. Review the overwritten
PNG in version control before committing so an unintended change isn't blessed.

## Tolerance: `threshold` vs `pixel_sensitivity`

These are two **independent** knobs. They answer different questions:

| Knob                | Question it answers                                  | Range     | Default |
| ------------------- | ---------------------------------------------------- | --------- | ------- |
| `pixel_sensitivity` | How *different* must a single pixel be to count?     | 0.0–1.0   | 0.0     |
| `threshold`         | How *many* counted pixels are tolerated overall?     | 0.0–1.0   | 0.0     |

### `pixel_sensitivity` — per-pixel color tolerance

A pixel is flagged as differing only when one of its RGBA channels differs from
the baseline by **more than** `pixel_sensitivity` (channels normalized to
`[0,1]`). It filters *individual* pixels before they are counted.

- `0.0` (default): exact color match — any channel difference flags the pixel.
- `0.05`: ignores per-channel drift up to ~13/255 — absorbs JPEG-like
  compression noise, subtle gradients, and minor anti-aliasing color shifts.
- `1.0`: every per-pixel delta is tolerated, so `diff_pixels` is always 0.

### `threshold` — image-level diff gate

After per-pixel filtering, `diff_ratio = diff_pixels / total_pixels`. The diff
**fails when `diff_ratio > threshold`**.

- `0.0` (default): any single differing pixel fails the diff (strictest).
- `0.01`: tolerates up to 1% of pixels changing — useful for blinking cursors,
  small animated indicators, or sub-pixel text rendering jitter.
- `1.0`: never fails on pixel count.

### Worked examples

```
# Strictest — pixel-perfect. Good for static UI panels.
godot_screenshot_diff(name="settings", threshold=0.0, pixel_sensitivity=0.0)

# Tolerate anti-aliasing color drift, but no large regions may change.
godot_screenshot_diff(name="hud", threshold=0.0, pixel_sensitivity=0.05)

# Allow a small animated badge (≤2% of the frame) to differ.
godot_screenshot_diff(name="main_menu", threshold=0.02, pixel_sensitivity=0.0)

# Lenient on both axes — smoke check that the scene didn't break entirely.
godot_screenshot_diff(name="world", threshold=0.10, pixel_sensitivity=0.05)
```

`max_delta` in the result reports the single largest per-channel difference seen
(in `[0,1]`), which helps you pick a `pixel_sensitivity` value: if a known-good
re-render reports `max_delta = 0.03`, set `pixel_sensitivity` just above it.

## Selector-cropped screenshots

Pass a `selector` to both save and diff to limit the capture to one node's
bounding rect (e.g. `godot_screenshot_save_baseline(name="hud", selector="HUD")`).
This isolates a region so unrelated on-screen motion doesn't trip the diff.

- Use the **same selector** for save and diff. The captured bounds must match,
  or the comparison errors with an image-size mismatch.
- Selectors are validated server-side before capture; an invalid selector
  returns an error without touching Godot.

## Failed-diff artifacts

On failure, the server writes two PNGs to the **artifact directory**, default
`stagehand-diffs/`:

| File                  | Contents                                                          |
| --------------------- | ----------------------------------------------------------------- |
| `<name>-actual.png`   | The frame just captured — what the game rendered this run.        |
| `<name>-diff.png`     | Visualization: differing pixels in **red**, matching pixels dimmed to ¼ brightness so changes stand out. |

Their paths are returned in the structured result (`actual_image_path`,
`diff_image_path`). A passing diff writes **no** artifacts. Re-running a failing
diff with the same name overwrites the prior artifacts.

## Machine-readable result fields

`godot_screenshot_diff` returns MCP `structuredContent` so agents can branch
without parsing prose. Shape:

```json
{
  "name": "main_menu",
  "pass": false,
  "total_pixels": 921600,
  "diff_pixels": 14820,
  "diff_ratio": 0.0161,
  "max_delta": 0.83,
  "threshold": 0.0,
  "pixel_sensitivity": 0.0,
  "width": 1280,
  "height": 720,
  "baseline_path": "stagehand-baselines/main_menu.png",
  "actual_image_path": "stagehand-diffs/main_menu-actual.png",
  "diff_image_path": "stagehand-diffs/main_menu-diff.png",
  "selector": ""
}
```

- **`pass`** is the single boolean to branch on. When `true`, the `*_image_path`
  fields are omitted (no artifacts were written).
- A failing diff also sets the result's error flag, so callers that only check
  MCP `isError` still see the regression; the human-readable text mirrors the
  fields above.

`godot_screenshot_save_baseline` likewise returns structured fields:
`name`, `path`, `width`, `height`, and `selector` (when cropped).

## Proof

`internal/mcpserver/tools_visual_test.go` is the canonical end-to-end proof: it
saves a baseline frame, changes the rendered state, runs a diff, and asserts a
FAILING result with on-disk artifacts and the machine-readable fields above —
all on real PNG pixels, with no game-specific assertions.

## Setting up a visual gate in your game repo

See [docs/visual-smoke-contract.md](visual-smoke-contract.md) for the
boundary between Stagehand and downstream game repos: what Stagehand guarantees,
what your repo owns, headless support status, version pinning, and a minimal
harness example.
