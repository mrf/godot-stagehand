class_name StagehandInputRecorder
extends Node
## Records input events with timestamps and replays them from saved files.
## Used by the Stagehand server to implement record/replay automation.
##
## The on-disk format is versioned; see docs/recording-format.md. Recordings
## written before that format was named used `frames`/`time_ms` instead of
## `events`/`t_ms`, and [method parse_recording] still reads them.

const ERRORS := preload("res://addons/stagehand/core/errors.gd")
const INPUT_SIMULATOR := preload("res://addons/stagehand/core/input_simulator.gd")
const SELF_PROTECTION := preload("res://addons/stagehand/core/self_protection.gd")

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

## Lifts the [method resolve_recording_path] sandbox so record and replay take
## any path. The server sets it from STAGEHAND_ALLOW_UNSAFE=1.
var allow_unsafe_paths: bool = false

var _recording: bool = false
var _include_mouse_move: bool = false
var _output_path: String = ""
var _session_id: String = ""
var _start_scene: String = ""
var _events: Array[Dictionary] = []
var _start_time_ms: int = 0
var _tap: _CaptureTap = null


## Feeds the recorder from the front of the root viewport's [code]_input[/code]
## pass. Godot calls [method Node._input] in reverse tree order, so the
## recorder's own position (under the StagehandServer autoload, ahead of the
## current scene) would see an event only after every game node had had the
## chance to [method Viewport.set_input_as_handled] it — a pause menu's Escape
## never reached the recording. An [constant Node.INTERNAL_MODE_BACK] child of
## the root sorts after every ordinary node, including scenes added later.
## [constant Node.PROCESS_MODE_ALWAYS] keeps capture running while the tree is
## paused. Viewport-level hooks such as [signal Window.window_input] were
## rejected: they fire before the root's stretch transform and never see the
## [method Viewport.push_input] events Stagehand itself injects.
class _CaptureTap extends Node:
	var sink: Callable

	func _input(event: InputEvent) -> void:
		if sink.is_valid():
			sink.call(event)


## Records [param event] if a recording is active. Positions are taken as
## delivered to [code]_input[/code]: the root viewport's canvas space, after
## content-scale stretch — the space [method start_replay] pushes them back in.
func capture(event: InputEvent) -> void:
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
		return ERRORS.make(ERRORS.RECORDER_STATE, "Already recording", {
			"session_id": _session_id,
			"next_action": "Call record_stop before starting another recording.",
		})
	var target: String = output_path
	if not target.is_empty():
		var resolved: Dictionary = _resolve_path(target)
		if ERRORS.is_error(resolved):
			return resolved
		target = resolved.get("path", "")
	_session_id = _next_session_id()
	_output_path = target if not target.is_empty() else default_output_path(_session_id)
	_include_mouse_move = include_mouse_move
	_start_scene = _current_scene_path()
	_events = []
	_start_time_ms = Time.get_ticks_msec()
	_recording = true
	_attach_tap()
	return {
		"success": true,
		"recording": true,
		"session_id": _session_id,
		"output_path": _output_path,
	}


## Stops capturing and writes the recording to its output path.
func stop_recording() -> Dictionary:
	if not _recording:
		return ERRORS.make(ERRORS.RECORDER_STATE, "Not recording", {
			"next_action": "Call record_start before record_stop.",
		})
	_recording = false
	_detach_tap()
	var duration_ms: int = Time.get_ticks_msec() - _start_time_ms
	var events_count: int = _events.size()
	var session_id: String = _session_id
	var path: String = _output_path
	var document: Dictionary = build_recording(_events, session_id, _start_scene, duration_ms)
	_events = []

	var dir_error: String = _ensure_parent_dir(path)
	if not dir_error.is_empty():
		return ERRORS.make(ERRORS.IO_ERROR, dir_error, {
			"path": path,
			"next_action": "Choose an output_path under a directory the game process can create and write.",
		})
	var file: FileAccess = FileAccess.open(path, FileAccess.WRITE)
	if file == null:
		var err: Error = FileAccess.get_open_error()
		return ERRORS.make(
			ERRORS.IO_ERROR,
			"Failed to open file for writing: %s (%s)" % [path, error_string(err)],
			{
				"path": path,
				"godot_error": error_string(err),
				"next_action": "Choose an output_path the game process can write, such as one under user://.",
			}
		)
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
		return ERRORS.make(ERRORS.RECORDER_STATE, "Cannot replay while recording", {
			"session_id": _session_id,
			"next_action": "Call record_stop before replaying.",
		})
	# Resolved before the open, so a refused path never reveals whether the
	# file exists or parses.
	var resolved: Dictionary = _resolve_path(input_path)
	if ERRORS.is_error(resolved):
		return resolved
	var path: String = resolved.get("path", "")
	var file: FileAccess = FileAccess.open(path, FileAccess.READ)
	if file == null:
		var err: Error = FileAccess.get_open_error()
		return ERRORS.make(
			ERRORS.IO_ERROR,
			"Failed to open file for reading: %s (%s)" % [path, error_string(err)],
			{
				"path": path,
				"godot_error": error_string(err),
				"next_action": "Check the recording_path; record_stop reports the path it wrote.",
			}
		)
	var text: String = file.get_as_text()
	file.close()

	var json: JSON = JSON.new()
	var parse_err: Error = json.parse(text)
	if parse_err != OK:
		return ERRORS.make(
			ERRORS.IO_ERROR,
			"Failed to parse recording: %s" % json.get_error_message(),
			{
				"path": path,
				"next_action": "Replay a file written by record_stop; it must be valid JSON.",
			}
		)
	var parsed: Dictionary = parse_recording(json.data)
	if ERRORS.is_error(parsed):
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
		var tree: SceneTree = get_tree()
		if delay_sec <= 0.0 or tree == null:
			_inject(event)
			continue
		@warning_ignore("return_value_discarded")
		tree.create_timer(delay_sec, true, false, true).timeout.connect(func() -> void:
			_inject(event)
		)

	if last_delay_sec > 0.0:
		var tree: SceneTree = get_tree()
		if tree != null:
			await tree.create_timer(last_delay_sec + REPLAY_TAIL_SEC, true, false, true).timeout

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
## an older build still replays. Returns `{"events": Array}` on success, or a
## canonical failure envelope (see core/errors.gd) on a malformed recording.
static func parse_recording(data: Variant) -> Dictionary:
	if data is not Dictionary:
		return ERRORS.make(
			ERRORS.IO_ERROR, "Invalid recording format: expected a JSON object", {
				"next_action": "Replay a file written by record_stop.",
			}
		)
	var document: Dictionary = data
	var version: int = _variant_to_int(document.get("version", FORMAT_VERSION))
	if version > FORMAT_VERSION:
		return ERRORS.make(
			ERRORS.IO_ERROR,
			"Unsupported recording version %d; this build reads up to version %d"
				% [version, FORMAT_VERSION],
			{
				"recording_version": version,
				"supported_version": FORMAT_VERSION,
				"next_action": "Re-record with this build, or upgrade the addon to one that reads this format.",
			}
		)

	var raw: Variant = document.get("events")
	if raw == null:
		raw = document.get("frames", [])
	if raw is not Array:
		return ERRORS.make(
			ERRORS.IO_ERROR, "Invalid recording format: \"events\" must be an array", {
				"next_action": "Replay a file written by record_stop.",
			}
		)
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


## Maps a caller-supplied recording path onto the file it names inside
## [constant DEFAULT_RECORDING_DIR]: [code]{"path": resolved}[/code], or an
## [code]invalid_params[/code] envelope when it would escape it. A path already
## under the directory is kept; another scheme (res://, a bare user://), an
## absolute path, a drive letter, a backslash, and a "." or ".." segment are
## refused rather than normalised. Mirrors Go's gwpop.ResolveRecordingPath —
## change both together (docs/security.md).
static func resolve_recording_path(path: String) -> Dictionary:
	var prefix: String = DEFAULT_RECORDING_DIR + "/"
	var rel: String = path
	if path.begins_with(prefix):
		rel = path.substr(prefix.length())
	elif path.contains(":"):
		return _invalid_recording_path(path, "only paths under %s may name a scheme or drive" % prefix)
	elif path.begins_with("/"):
		return _invalid_recording_path(path, "absolute paths are not allowed")
	if rel.is_empty():
		return _invalid_recording_path(path, "a file name is required")
	for segment: String in rel.split("/"):
		if segment.is_empty() or segment == "." or segment == "..":
			return _invalid_recording_path(path, "segment \"%s\" is not allowed" % segment)
		for i: int in segment.length():
			if not _is_recording_path_char(segment.unicode_at(i)):
				return _invalid_recording_path(path, "character \"%s\" is not allowed" % segment[i])
	return {"path": prefix + rel}


## Path used when the caller starts a recording without naming one.
static func default_output_path(session_id: String) -> String:
	return "%s/%s.json" % [DEFAULT_RECORDING_DIR, session_id]


static func serialize_event(event: InputEvent) -> Dictionary:
	if event is InputEventKey:
		var e: InputEventKey = event
		return {
			"type": "key",
			"keycode": e.keycode,
			"physical_keycode": e.physical_keycode,
			"key_label": e.key_label,
			"unicode": e.unicode,
			"pressed": e.pressed,
			"echo": e.echo,
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
			ev.physical_keycode = _variant_to_int(entry.get("physical_keycode", 0)) as Key
			ev.key_label = _variant_to_int(entry.get("key_label", 0)) as Key
			ev.unicode = _variant_to_int(entry.get("unicode", 0))
			ev.pressed = _variant_to_bool(entry.get("pressed", false))
			ev.echo = _variant_to_bool(entry.get("echo", false))
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


## Pointer events were captured in the root's canvas space, so they go back in
## through the simulator's [method Viewport.push_input] path:
## [method Input.parse_input_event] would treat them as window pixels and apply
## content-scale stretch a second time. Touch is not re-emulated as mouse there,
## which is right here — any emulated mouse events were recorded alongside it.
## Keys and actions keep [method Input.parse_input_event], which routes them to
## the focused window (see [method StagehandInputSimulator.focus_window]).
func _inject(event: InputEvent) -> void:
	if (
		event is InputEventMouse
		or event is InputEventScreenTouch
		or event is InputEventScreenDrag
	):
		var tree: SceneTree = get_tree()
		INPUT_SIMULATOR.push_local(tree.root if tree != null else null, event)
		return
	Input.parse_input_event(event)


func _attach_tap() -> void:
	var tree: SceneTree = get_tree()
	if tree == null:
		return
	_tap = _CaptureTap.new()
	_tap.name = "StagehandInputRecorderTap"
	_tap.sink = capture
	_tap.process_mode = Node.PROCESS_MODE_ALWAYS
	_tap.add_to_group(SELF_PROTECTION.PROTECTED_GROUP)
	tree.root.add_child(_tap, false, Node.INTERNAL_MODE_BACK)


func _detach_tap() -> void:
	if _tap == null:
		return
	_tap.sink = Callable()
	_tap.queue_free()
	_tap = null


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


## [method resolve_recording_path], unless [member allow_unsafe_paths] lifts the
## sandbox and the path is taken verbatim.
func _resolve_path(path: String) -> Dictionary:
	if allow_unsafe_paths:
		return {"path": path}
	var resolved: Dictionary = resolve_recording_path(path)
	if ERRORS.is_error(resolved):
		return resolved
	var link: String = find_symlink_in_recording_path(str(resolved.get("path", "")))
	if not link.is_empty():
		return _invalid_recording_path(path, "%s is a symlink, which could redirect the recording outside %s/" % [link, DEFAULT_RECORDING_DIR])
	return resolved


## The first symlink on the way from [constant DEFAULT_RECORDING_DIR] down to
## [param resolved_path] (the directory itself included), or "" if there is
## none. [method resolve_recording_path] is lexical, so this is the filesystem
## half of the sandbox; a component that does not exist yet is not a link.
static func find_symlink_in_recording_path(resolved_path: String) -> String:
	var dir: DirAccess = DirAccess.open("user://")
	if dir == null:
		return ""
	var current: String = DEFAULT_RECORDING_DIR
	if dir.is_link(current):
		return current
	for part: String in resolved_path.trim_prefix(DEFAULT_RECORDING_DIR + "/").split("/"):
		current = current + "/" + part
		if dir.is_link(current):
			return current
	return ""


static func _invalid_recording_path(path: String, reason: String) -> Dictionary:
	return ERRORS.make(
		ERRORS.INVALID_PARAMS,
		"Invalid recording path \"%s\": %s" % [path, reason],
		{
			"path": path,
			"next_action": "Use a path inside %s/, e.g. \"run1.json\" or \"runs/run1.json\"; segments use letters, digits, '_', '-' and '.'. STAGEHAND_ALLOW_UNSAFE=1 lifts this." % DEFAULT_RECORDING_DIR,
		}
	)


static func _is_recording_path_char(c: int) -> bool:
	return (
		(c >= 97 and c <= 122)  # a-z
		or (c >= 65 and c <= 90)  # A-Z
		or (c >= 48 and c <= 57)  # 0-9
		or c == 95  # _
		or c == 45  # -
		or c == 46  # .
	)


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
