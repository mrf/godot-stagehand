# Screenshot Pixel Proof (canonical)

This is the **canonical upstream proof** that `godot_screenshot` returns real,
rendered pixels — not metadata, an empty buffer, or a grey/black frame.

Downstream projects doing visual-smoke testing should point here when a
screenshot comes back blank: if this proof passes, Stagehand's capture →
transport → PNG decode path is sound, so a blank downstream screenshot is the
downstream scene or that project's own display plumbing, **not** Stagehand.

## What proves it

- **Fixture:** `testdata/screenshot_fixture/` — a tiny project whose only scene
  (`scenes/pixel_fixture.tscn`) paints four full-coverage `ColorRect` quadrants
  in unmistakable primary colors:

  | quadrant      | color |
  |---------------|-------|
  | top-left      | red   `(255,0,0)`   |
  | top-right     | green `(0,255,0)`   |
  | bottom-left   | blue  `(0,0,255)`   |
  | bottom-right  | white `(255,255,255)` |

  Anchors are fractional, so the quadrants fill whatever the actual viewport
  size is; sampling uses fractions of the captured dimensions.

- **Test:** `internal/launch/screenshot_fixture_test.go`
  (`TestScreenshotPixelFixtureProvesRealRender`). It launches the fixture on a
  **visible display** (not headless), captures a full-viewport screenshot
  through the live addon `screenshot` method, decodes the PNG, and asserts the
  sampled quadrant pixels match the expected colors. Only a genuinely rendered
  frame can satisfy that.

- **Pixel analysis:** `internal/imgdiff/content.go` (`Summarize` +
  `BlankDiagnosis`, unit-tested in `content_test.go`). This is the reusable
  logic that distinguishes a meaningful frame from an all-black /
  all-transparent / single-color blank, and is exercised everywhere (no Godot
  required) so the assertion logic itself is proven in CI.

## Required display path

A **real rendered frame requires a headed Godot process on a working display**:

- A local desktop session (X11 or Wayland).
- **WSLg** — Windows Subsystem for Linux's GUI support exports both `DISPLAY`
  and `WAYLAND_DISPLAY`; this is the supported local path on WSL.
- A virtual framebuffer such as **Xvfb** with a GPU or software rasterizer, for
  CI.

`--headless` Godot uses the dummy rendering driver and **cannot** produce real
pixels; the addon reports `viewport_image_empty` in that case.

## Honest gating (why it sometimes skips vs. fails)

The proof is deliberately *not* behind the `integration` build tag — it runs in
the normal `go test ./...` and self-skips where a real frame cannot exist:

- short mode / no Godot binary / no display server → **skip** (frame not
  obtainable here).
- addon reports it could not produce a frame (`viewport_image_empty`, etc.) →
  **skip**, surfacing the addon diagnostic (no real render on this path —
  headless dummy driver / no GPU).
- addon returns image **data** but the pixels are blank / black / transparent /
  uniform, or the wrong colors → **fail loudly** with an actionable diagnosis.
  A frame was produced and is expected to be meaningful; a blank one is the
  regression this proof exists to catch.

## Related fix

This proof surfaced a real addon bug: `StagehandCommandRouter.dispatch()` was
synchronous and returned `null` for coroutine handlers (`screenshot`,
`input_text`) that suspend on `await`. The server (`stagehand_server.gd`) now
awaits the handler `Callable` directly via `StagehandCommandRouter.get_handler()`,
so coroutine results are returned instead of `null`.
