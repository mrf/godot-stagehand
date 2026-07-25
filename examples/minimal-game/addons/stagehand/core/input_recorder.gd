class_name StagehandInputRecorder
extends Node
## Records input events with timestamps and replays them from saved files.
## Used by the Stagehand server to implement record/replay automation.
##
## The on-disk format is versioned; see docs/recording-format.md. Recordings
## written before that format was named used `frames`/`time_ms` instead of
## `events`/`t_ms`, and [method parse_recording] still reads them.

## Recording format generation this build writes. Readers accept anything at or
## below it; a higher version is refused rather than silently mis-read.
const FORMAT_VERSION: int = 1

## Frames [method start_replay] waits for the scene tree to produce a current
## scene before giving up and injecting anyway.
const READY_MAX_FRAMES: int = 240

## Grace period after the last scheduled event, so its timer has fired before
## the replay reports completion.
const REPLAY_TAIL_SEC: float = 0.05

## Directory for recordings when the caller does not name an output path.
const DEFAULT_RECORDING_DIR: String = "user://stagehand_recordings"

static var _session_counter: int = 0

var _recording: bool = false
var _include_mouse_move: bool = false
var _output_path: String = ""
var _session_id: String = ""
var _start_scene: String = ""
var _events: Array[Dictionary] = []
var _start_time_ms: int = 0


func _input(event: InputEvent) -> void:
	if not _recording:
		return
	if not _include_mouse_move and event is InputEventMouseMotion:
		return
	var serialized: Dictionary = serialize_event(event)
	if serialized.is_empty():
		return
	serialized["t_ms"] = Time.get_ticks_msec() - _start_time_ms
	_events.append(serialized)


func _exit_tree() -> void:
	if _recording:
		var _result: Dictionary = stop_recording()


## Whether a recording is currently capturing input.
func is_recording() -> bool:
	return _recording


## Events captured so far in the active recording. Used by tests to observe the
## capture filter without going through a file.
func pending_event_count() -> int:
	return _events.size()


## Begins capturing input. [param output_path] may be empty, in which case a
## timestamped file under [constant DEFAULT_RECORDING_DIR] is chosen. Mouse
## motion is dropped unless [param include_mouse_move] is true — it dominates a
## recording's size and is rarely what a repro depends on.
func start_recording(output_path: String, include_mouse_move: bool = false) -> Dictionary:
	if _recording:
		return {"error": "Already recording"}
	_session_id = _next_session_id()
	_output_path = output_path if not output_path.is_empty() else default_output_path(_session_id)
	_include_mouse_move = include_mouse_move
	_start_scene = _current_scene_path()
	_events = []
	_start_time_ms = Time.get_ticks_msec()
	_recording = true
	return {
		"success": true,
		"recording": true,
		"session_id": _session_id,
		"output_path": _output_path,
	}


## Stops capturing and writes the recording to its output path.
func stop_recording() -> Dictionary:
	if not _recording:
		return {"error": "Not recording"}
	_recording = false
	var duration_ms: int = Time.get_ticks_msec() - _start_time_ms
	var events_count: int = _events.size()
	var session_id: String = _session_id
	var path: String = _output_path
	var document: Dictionary = build_recording(_events, session_id, _start_scene, duration_ms)
	_events = []

	var dir_error: String = _ensure_parent_dir(path)
	if not dir_error.is_empty():
		return {"error": dir_error}
	var file: FileAccess = FileAccess.open(path, FileAccess.WRITE)
	if file == null:
		var err: Error = FileAccess.get_open_error()
		return {"error": "Failed to open file for writing: %s (%s)" % [path, error_string(err)]}
	@warning_ignore("return_value_discarded")
	file.store_string(JSON.stringify(document, "\t"))
	file.close()

	return {
		"success": true,
		"session_id": session_id,
		"events_count": events_count,
		"duration_ms": duration_ms,
		"path": path,
	}


## Replays a recording, injecting each event at its recorded offset divided by
## [param speed]. Every event is scheduled up front against one clock, so the
## relative timing of the whole sequence is preserved rather than accumulating
## per-event drift.
func start_replay(input_path: String, speed: float = 1.0, wait_for_ready: bool = true) -> Dictionary:
	if _recording:
		return {"error": "Cannot replay while recording"}
	var file: FileAccess = FileAccess.open(input_path, FileAccess.READ)
	if file == null:
		var err: Error = FileAccess.get_open_error()
		return {"error": "Failed to open file for reading: %s (%s)" % [input_path, error_string(err)]}
	var text: String = file.get_as_text()
	file.close()

	var json: JSON = JSON.new()
	var parse_err: Error = json.parse(text)
	if parse_err != OK:
		return {"error": "Failed to parse recording: %s" % json.get_error_message()}
	var parsed: Dictionary = parse_recording(json.data)
	if parsed.has("error"):
		return parsed
	var events: Array = parsed.get("events", [])

	if wait_for_ready:
		await _await_scene_ready()

	var started_ms: int = Time.get_ticks_msec()
	var replayed_count: int = 0
	var last_delay_sec: float = 0.0
	for event_variant: Variant in events:
		if event_variant is not Dictionary:
			continue
		var entry: Dictionary = event_variant
		var event: InputEvent = deserialize_event(entry)
		if event == null:
			continue
		replayed_count += 1
		var delay_sec: float = event_delay_sec(_variant_to_int(entry.get("t_ms", 0)), speed)
		if delay_sec > last_delay_sec:
			last_delay_sec = delay_sec
		if delay_sec <= 0.0:
			Input.parse_input_event(event)
			continue
		var tree: SceneTree = get_tree()
		if tree == null:
			Input.parse_input_event(event)
			continue
		@warning_ignore("return_value_discarded")
		tree.create_timer(delay_sec).timeout.connect(func() -> void:
			Input.parse_input_event(event)
		)

	if last_delay_sec > 0.0:
		var tree: SceneTree = get_tree()
		if tree != null:
			await tree.create_timer(last_delay_sec + REPLAY_TAIL_SEC).timeout

	return {
		"success": true,
		"replayed": true,
		"events_count": replayed_count,
		"duration_ms": Time.get_ticks_msec() - started_ms,
	}


## Wraps captured events in the versioned document that gets written to disk.
static func build_recording(
	events: Array, session_id: String, start_scene: String, duration_ms: int
) -> Dictionary:
	var version_info: Dictionary = Engine.get_version_info()
	return {
		"version": FORMAT_VERSION,
		"session_id": session_id,
		"start_scene": start_scene,
		"engine_version": str(version_info.get("string", "")),
		"duration_ms": duration_ms,
		"events": events,
	}


## Validates a decoded recording and normalizes its events to the current
## spelling. Accepts the legacy `frames`/`time_ms` keys so a recording made by
## an older build still replays. Returns `{"events": Array}` or `{"error": String}`.
static func parse_recording(data: Variant) -> Dictionary:
	if data is not Dictionary:
		return {"error": "Invalid recording format: expected a JSON object"}
	var document: Dictionary = data
	var version: int = _variant_to_int(document.get("version", FORMAT_VERSION))
	if version > FORMAT_VERSION:
		return {
			"error": "Unsupported recording version %d; this build reads up to version %d"
			% [version, FORMAT_VERSION]
		}

	var raw: Variant = document.get("events")
	if raw == null:
		raw = document.get("frames", [])
	if raw is not Array:
		return {"error": "Invalid recording format: \"events\" must be an array"}
	var raw_events: Array = raw

	var normalized: Array[Dictionary] = []
	for item: Variant in raw_events:
		if item is not Dictionary:
			continue
		var source: Dictionary = item
		var entry: Dictionary = source.duplicate()
		# JSON hands every number back as a float, so coerce unconditionally:
		# callers are promised an int `t_ms` whatever the file said.
		var raw_offset: Variant = entry.get("t_ms", entry.get("time_ms", 0))
		entry["t_ms"] = _variant_to_int(raw_offset)
		normalized.append(entry)
	return {"events": normalized}


## Wall-clock delay for an event recorded at [param t_ms], under a playback
## [param speed] multiplier. A non-positive speed is meaningless, so it falls
## back to realtime rather than producing a negative or infinite delay.
static func event_delay_sec(t_ms: int, speed: float) -> float:
	var effective_speed: float = speed if speed > 0.0 else 1.0
	return (float(t_ms) / 1000.0) / effective_speed


## Path used when the caller starts a recording without naming one.
static func default_output_path(session_id: String) -> String:
	return "%s/%s.json" % [DEFAULT_RECORDING_DIR, session_id]


static func serialize_event(event: InputEvent) -> Dictionary:
	if event is InputEventKey:
		var e: InputEventKey = event
		return {
			"type": "key",
			"keycode": e.keycode,
			"unicode": e.unicode,
			"pressed": e.pressed,
			"shift": e.shift_pressed,
			"ctrl": e.ctrl_pressed,
			"alt": e.alt_pressed,
			"meta": e.meta_pressed,
		}
	elif event is InputEventMouseButton:
		var e: InputEventMouseButton = event
		return {
			"type": "mouse_button",
			"position": {"x": e.position.x, "y": e.position.y},
			"button_index": e.button_index,
			"pressed": e.pressed,
			"double_click": e.double_click,
		}
	elif event is InputEventMouseMotion:
		var e: InputEventMouseMotion = event
		return {
			"type": "mouse_motion",
			"position": {"x": e.position.x, "y": e.position.y},
			"relative": {"x": e.relative.x, "y": e.relative.y},
		}
	elif event is InputEventAction:
		var e: InputEventAction = event
		return {
			"type": "action",
			"action": e.action,
			"strength": e.strength,
			"pressed": e.pressed,
		}
	elif event is InputEventScreenTouch:
		var e: InputEventScreenTouch = event
		return {
			"type": "screen_touch",
			"position": {"x": e.position.x, "y": e.position.y},
			"index": e.index,
			"pressed": e.pressed,
		}
	elif event is InputEventScreenDrag:
		var e: InputEventScreenDrag = event
		return {
			"type": "screen_drag",
			"position": {"x": e.position.x, "y": e.position.y},
			"index": e.index,
			"relative": {"x": e.relative.x, "y": e.relative.y},
		}
	return {}


static func deserialize_event(entry: Dictionary) -> InputEvent:
	var type: String = entry.get("type", "")
	match type:
		"key":
			var ev: InputEventKey = InputEventKey.new()
			ev.keycode = _variant_to_int(entry.get("keycode", 0)) as Key
			ev.unicode = _variant_to_int(entry.get("unicode", 0))
			ev.pressed = _variant_to_bool(entry.get("pressed", false))
			ev.shift_pressed = _variant_to_bool(entry.get("shift", false))
			ev.ctrl_pressed = _variant_to_bool(entry.get("ctrl", false))
			ev.alt_pressed = _variant_to_bool(entry.get("alt", false))
			ev.meta_pressed = _variant_to_bool(entry.get("meta", false))
			return ev
		"mouse_button":
			var ev: InputEventMouseButton = InputEventMouseButton.new()
			ev.position = _variant_to_vector2(entry.get("position", {}))
			ev.global_position = ev.position
			ev.button_index = _variant_to_int(entry.get("button_index", MOUSE_BUTTON_LEFT)) as MouseButton
			ev.pressed = _variant_to_bool(entry.get("pressed", false))
			ev.double_click = _variant_to_bool(entry.get("double_click", false))
			return ev
		"mouse_motion":
			var ev: InputEventMouseMotion = InputEventMouseMotion.new()
			ev.position = _variant_to_vector2(entry.get("position", {}))
			ev.global_position = ev.position
			ev.relative = _variant_to_vector2(entry.get("relative", {}))
			return ev
		"action":
			var ev: InputEventAction = InputEventAction.new()
			ev.action = str(entry.get("action", ""))
			ev.strength = _variant_to_float(entry.get("strength", 1.0))
			ev.pressed = _variant_to_bool(entry.get("pressed", false))
			return ev
		"screen_touch":
			var ev: InputEventScreenTouch = InputEventScreenTouch.new()
			ev.position = _variant_to_vector2(entry.get("position", {}))
			ev.index = _variant_to_int(entry.get("index", 0))
			ev.pressed = _variant_to_bool(entry.get("pressed", false))
			return ev
		"screen_drag":
			var ev: InputEventScreenDrag = InputEventScreenDrag.new()
			ev.position = _variant_to_vector2(entry.get("position", {}))
			ev.index = _variant_to_int(entry.get("index", 0))
			ev.relative = _variant_to_vector2(entry.get("relative", {}))
			return ev
	return null


## Blocks until the scene tree has a current scene, so the first injected event
## does not land before the scene under test exists.
func _await_scene_ready() -> void:
	var tree: SceneTree = get_tree()
	if tree == null:
		return
	var frames: int = 0
	while tree.current_scene == null and frames < READY_MAX_FRAMES:
		await tree.process_frame
		frames += 1
	await tree.process_frame


func _current_scene_path() -> String:
	var tree: SceneTree = get_tree()
	if tree == null or tree.current_scene == null:
		return ""
	return tree.current_scene.scene_file_path


static func _next_session_id() -> String:
	_session_counter += 1
	return "rec-%d-%d" % [Time.get_ticks_usec(), _session_counter]


## Creates the directory holding [param path] if it is missing. Returns an empty
## string on success, or the error message to report.
static func _ensure_parent_dir(path: String) -> String:
	var dir: String = path.get_base_dir()
	if dir.is_empty() or DirAccess.dir_exists_absolute(dir):
		return ""
	var err: Error = DirAccess.make_dir_recursive_absolute(dir)
	if err != OK:
		return "Failed to create directory: %s (%s)" % [dir, error_string(err)]
	return ""


static func _variant_to_vector2(v: Variant) -> Vector2:
	if v is not Dictionary:
		return Vector2.ZERO
	var d: Dictionary = v
	return Vector2(_variant_to_float(d.get("x", 0.0)), _variant_to_float(d.get("y", 0.0)))


static func _variant_to_int(v: Variant) -> int:
	if v is int:
		return v
	if v is float:
		var f: float = v
		return int(f)
	return 0


static func _variant_to_float(v: Variant) -> float:
	if v is float:
		return v
	if v is int:
		var i: int = v
		return float(i)
	return 0.0


static func _variant_to_bool(v: Variant) -> bool:
	if v is bool:
		return v
	return false
