# Input recording format

`godot_record_stop` writes a single JSON document describing one captured input
session. `godot_replay` reads it back. The format is versioned so a recording
outlives the build that produced it.

Canonical implementation: `addons/stagehand/core/input_recorder.gd`
(`build_recording` writes, `parse_recording` reads). Unit tests pinning every
rule below live in `testdata/test_project/test/test_input_recorder.gd`.

## Document

```json
{
  "version": 1,
  "session_id": "rec-318590-1",
  "start_scene": "res://scenes/main.tscn",
  "engine_version": "4.6.2.stable.official",
  "duration_ms": 1500,
  "events": [
    {"t_ms": 0,   "type": "key",          "keycode": 87, "pressed": true},
    {"t_ms": 500, "type": "mouse_button", "position": {"x": 512, "y": 300},
     "button_index": 1, "pressed": true}
  ]
}
```

| Field | Type | Meaning |
| --- | --- | --- |
| `version` | int | Format generation. See [Versioning](#versioning). |
| `session_id` | string | Identifies the capture; echoed by `record_start` and `record_stop`. |
| `start_scene` | string | `scene_file_path` of the scene that was current when recording began, or `""` if there was none. Advisory — replay does **not** load it. |
| `engine_version` | string | `Engine.get_version_info().string` at capture time. Advisory. |
| `duration_ms` | int | Wall-clock length of the capture. |
| `events` | array | Captured events, in capture order. |

## Events

Every event carries `t_ms` — an integer millisecond offset from the start of
the recording — and a `type` discriminator. Replay schedules each event at
`t_ms / speed`, all against one clock taken at replay start, so the sequence
keeps its relative timing instead of accumulating per-event drift.

| `type` | InputEvent | Fields beyond `t_ms`/`type` |
| --- | --- | --- |
| `key` | `InputEventKey` | `keycode`, `unicode`, `pressed`, `shift`, `ctrl`, `alt`, `meta` |
| `mouse_button` | `InputEventMouseButton` | `position {x,y}`, `button_index`, `pressed`, `double_click` |
| `mouse_motion` | `InputEventMouseMotion` | `position {x,y}`, `relative {x,y}` |
| `action` | `InputEventAction` | `action`, `strength`, `pressed` |
| `screen_touch` | `InputEventScreenTouch` | `position {x,y}`, `index`, `pressed` |
| `screen_drag` | `InputEventScreenDrag` | `position {x,y}`, `index`, `relative {x,y}` |

Any other `InputEvent` subclass is dropped at capture, and an unrecognized
`type` is skipped at replay rather than aborting the run — a recording made by
a newer build still replays its understood events on an older one.

`mouse_motion` is **not** captured unless `godot_record_start` is called with
`include_mouse_move: true`. Motion dominates a recording's size and is rarely
what a repro depends on.

### Numbers are read leniently

Godot's JSON parser returns every number as a float, so `version` and `t_ms`
come back as `1.0` and `250.0`. The reader coerces them, and callers of
`parse_recording` are promised an integer `t_ms` whatever the file contained.
Do not assume a round-trip preserves the JSON number type.

## Versioning

`FORMAT_VERSION` (currently `1`) is the generation this build writes. The
reader accepts anything at or below it and refuses anything above with an
error naming both versions — a newer recording is not silently mis-read.

Bump `FORMAT_VERSION` only for a change that an older reader would
misinterpret. Adding a new optional field, or a new event `type`, does not
qualify: unknown fields are ignored and unknown types are skipped.

### Legacy key names

Recordings written before this format was specified used `frames` instead of
`events` and `time_ms` instead of `t_ms`, with no `session_id`, `start_scene`,
`engine_version`, or `duration_ms`. They were also stamped `"version": 1`, so
the version number alone cannot distinguish them. `parse_recording` therefore
falls back to `frames`/`time_ms` when the current keys are absent, and such
files still replay. Nothing writes that spelling anymore.

## Where recordings go

`godot_record_start` takes an optional `output_path`. When it is omitted or
empty the recorder picks `user://stagehand_recordings/<session_id>.json`. The
parent directory is created if missing, for a caller-supplied path as well as
the default one.
