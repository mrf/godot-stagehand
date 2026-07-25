# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for StagehandInputRecorder — the recording format contract (versioned
## header, `t_ms`-keyed events), event serialization round-trips for every
## captured InputEvent family, the `include_mouse_move` capture filter, and
## replay's speed multiplier.
##
## The format tests are the load-bearing ones: a recording written by one build
## must stay readable by the next, so both the current `events`/`t_ms` spelling
## and the legacy `frames`/`time_ms` spelling are pinned here.

const RECORDER := preload("res://addons/stagehand/core/input_recorder.gd")

var _recorder: RECORDER
var _tmp_dir: String


func before_test() -> void:
	_recorder = RECORDER.new()
	add_child(_recorder)
	auto_free(_recorder)
	_tmp_dir = create_temp_dir("recordings")


func _tmp_path(file_name: String) -> String:
	return "%s/%s" % [_tmp_dir, file_name]


# --- serialization -----------------------------------------------------------


func test_serialize_key_event_captures_keycode_and_modifiers() -> void:
	var event: InputEventKey = InputEventKey.new()
	event.keycode = KEY_W
	event.pressed = true
	event.shift_pressed = true
	var out: Dictionary = RECORDER.serialize_event(event)
	assert_str(str(out.get("type"))).is_equal("key")
	assert_int(out.get("keycode")).is_equal(KEY_W)
	assert_bool(out.get("pressed")).is_true()
	assert_bool(out.get("shift")).is_true()
	assert_bool(out.get("ctrl")).is_false()


func test_serialize_mouse_button_event_captures_position_and_button() -> void:
	var event: InputEventMouseButton = InputEventMouseButton.new()
	event.position = Vector2(512, 300)
	event.button_index = MOUSE_BUTTON_LEFT
	event.pressed = true
	var out: Dictionary = RECORDER.serialize_event(event)
	assert_str(str(out.get("type"))).is_equal("mouse_button")
	assert_int(out.get("button_index")).is_equal(MOUSE_BUTTON_LEFT)
	var pos: Dictionary = out.get("position", {})
	assert_float(pos.get("x")).is_equal_approx(512.0, 0.01)
	assert_float(pos.get("y")).is_equal_approx(300.0, 0.01)


func test_serialize_action_event_captures_action_and_strength() -> void:
	var event: InputEventAction = InputEventAction.new()
	event.action = "ui_accept"
	event.strength = 0.5
	event.pressed = true
	var out: Dictionary = RECORDER.serialize_event(event)
	assert_str(str(out.get("type"))).is_equal("action")
	assert_str(str(out.get("action"))).is_equal("ui_accept")
	assert_float(out.get("strength")).is_equal_approx(0.5, 0.01)


func test_serialize_screen_touch_event_captures_index() -> void:
	var event: InputEventScreenTouch = InputEventScreenTouch.new()
	event.position = Vector2(10, 20)
	event.index = 2
	event.pressed = true
	var out: Dictionary = RECORDER.serialize_event(event)
	assert_str(str(out.get("type"))).is_equal("screen_touch")
	assert_int(out.get("index")).is_equal(2)


func test_serialize_ignores_unsupported_event_types() -> void:
	var event: InputEventMIDI = InputEventMIDI.new()
	assert_dict(RECORDER.serialize_event(event)).is_empty()


func test_round_trip_preserves_key_event() -> void:
	var event: InputEventKey = InputEventKey.new()
	event.keycode = KEY_A
	event.pressed = true
	event.ctrl_pressed = true
	var restored: InputEvent = RECORDER.deserialize_event(RECORDER.serialize_event(event))
	assert_object(restored).is_instanceof(InputEventKey)
	var key_event: InputEventKey = restored
	assert_int(key_event.keycode).is_equal(KEY_A)
	assert_bool(key_event.pressed).is_true()
	assert_bool(key_event.ctrl_pressed).is_true()


func test_round_trip_preserves_mouse_button_event() -> void:
	var event: InputEventMouseButton = InputEventMouseButton.new()
	event.position = Vector2(64, 128)
	event.button_index = MOUSE_BUTTON_RIGHT
	event.pressed = true
	var restored: InputEvent = RECORDER.deserialize_event(RECORDER.serialize_event(event))
	assert_object(restored).is_instanceof(InputEventMouseButton)
	var mouse_event: InputEventMouseButton = restored
	assert_vector(mouse_event.position).is_equal(Vector2(64, 128))
	assert_int(mouse_event.button_index).is_equal(MOUSE_BUTTON_RIGHT)


func test_deserialize_returns_null_for_unknown_type() -> void:
	assert_object(RECORDER.deserialize_event({"type": "not_a_real_event"})).is_null()


# --- recording format --------------------------------------------------------


func test_build_recording_writes_versioned_header() -> void:
	var doc: Dictionary = RECORDER.build_recording([], "sess-1", "res://scenes/main.tscn", 1500)
	assert_int(doc.get("version")).is_equal(RECORDER.FORMAT_VERSION)
	assert_str(str(doc.get("session_id"))).is_equal("sess-1")
	assert_str(str(doc.get("start_scene"))).is_equal("res://scenes/main.tscn")
	assert_int(doc.get("duration_ms")).is_equal(1500)
	assert_str(str(doc.get("engine_version"))).is_not_empty()
	assert_array(doc.get("events")).is_empty()


func test_build_recording_keys_events_by_t_ms() -> void:
	var events: Array[Dictionary] = [{"type": "key", "keycode": KEY_W, "t_ms": 250}]
	var doc: Dictionary = RECORDER.build_recording(events, "sess-1", "", 250)
	var stored: Array = doc.get("events", [])
	assert_array(stored).has_size(1)
	var first: Dictionary = stored[0]
	assert_int(first.get("t_ms")).is_equal(250)


func test_parse_recording_reads_current_format() -> void:
	var result: Dictionary = RECORDER.parse_recording({
		"version": 1,
		"events": [{"type": "key", "keycode": KEY_W, "t_ms": 100}],
	})
	assert_bool(result.has("error")).is_false()
	var events: Array = result.get("events", [])
	assert_array(events).has_size(1)
	var first: Dictionary = events[0]
	assert_int(first.get("t_ms")).is_equal(100)


func test_parse_recording_reads_legacy_frames_and_time_ms() -> void:
	# Recordings written before vrj.6 used `frames`/`time_ms`; they must still load.
	var result: Dictionary = RECORDER.parse_recording({
		"version": 1,
		"frames": [{"type": "key", "keycode": KEY_W, "time_ms": 100}],
	})
	assert_bool(result.has("error")).is_false()
	var events: Array = result.get("events", [])
	assert_array(events).has_size(1)
	var first: Dictionary = events[0]
	assert_int(first.get("t_ms")).is_equal(100)


func test_parse_recording_rejects_future_format_version() -> void:
	var result: Dictionary = RECORDER.parse_recording({"version": 99, "events": []})
	assert_str(str(result.get("error", ""))).contains("version")


func test_parse_recording_rejects_non_dictionary() -> void:
	assert_bool(RECORDER.parse_recording([]).has("error")).is_true()


# --- speed multiplier --------------------------------------------------------


func test_event_delay_sec_scales_with_speed() -> void:
	assert_float(RECORDER.event_delay_sec(1000, 1.0)).is_equal_approx(1.0, 0.001)
	assert_float(RECORDER.event_delay_sec(1000, 2.0)).is_equal_approx(0.5, 0.001)
	assert_float(RECORDER.event_delay_sec(1000, 0.5)).is_equal_approx(2.0, 0.001)


func test_event_delay_sec_treats_non_positive_speed_as_realtime() -> void:
	assert_float(RECORDER.event_delay_sec(1000, 0.0)).is_equal_approx(1.0, 0.001)
	assert_float(RECORDER.event_delay_sec(1000, -3.0)).is_equal_approx(1.0, 0.001)


# --- capture lifecycle -------------------------------------------------------


func test_start_recording_reports_session_id_and_recording_flag() -> void:
	var result: Dictionary = _recorder.start_recording(_tmp_path("run.json"), false)
	assert_bool(result.get("recording")).is_true()
	assert_str(str(result.get("session_id"))).is_not_empty()


func test_start_recording_generates_default_output_path_when_empty() -> void:
	var result: Dictionary = _recorder.start_recording("", false)
	assert_str(str(result.get("output_path"))).starts_with("user://")
	assert_str(str(result.get("output_path"))).ends_with(".json")


func test_session_ids_are_unique_across_recordings() -> void:
	var first: Dictionary = _recorder.start_recording(_tmp_path("a.json"), false)
	var _stop_a: Dictionary = _recorder.stop_recording()
	var second: Dictionary = _recorder.start_recording(_tmp_path("b.json"), false)
	var _stop_b: Dictionary = _recorder.stop_recording()
	assert_str(str(first.get("session_id"))).is_not_equal(str(second.get("session_id")))


func test_start_recording_rejects_a_second_start() -> void:
	var _first: Dictionary = _recorder.start_recording(_tmp_path("run.json"), false)
	assert_str(str(_recorder.start_recording(_tmp_path("other.json"), false).get("error", ""))).is_not_empty()


func test_stop_recording_without_start_is_an_error() -> void:
	assert_str(str(_recorder.stop_recording().get("error", ""))).is_not_empty()


func test_mouse_motion_is_dropped_by_default() -> void:
	var _start: Dictionary = _recorder.start_recording(_tmp_path("run.json"), false)
	_recorder._input(_motion_event())
	_recorder._input(_key_event(KEY_W))
	assert_int(_recorder.pending_event_count()).is_equal(1)


func test_mouse_motion_is_captured_when_requested() -> void:
	var _start: Dictionary = _recorder.start_recording(_tmp_path("run.json"), true)
	_recorder._input(_motion_event())
	_recorder._input(_key_event(KEY_W))
	assert_int(_recorder.pending_event_count()).is_equal(2)


func test_events_outside_a_recording_are_ignored() -> void:
	_recorder._input(_key_event(KEY_W))
	assert_int(_recorder.pending_event_count()).is_equal(0)


func test_stop_recording_reports_counts_and_writes_the_file() -> void:
	var path: String = _tmp_path("run.json")
	var _start: Dictionary = _recorder.start_recording(path, false)
	_recorder._input(_key_event(KEY_W))
	_recorder._input(_key_event(KEY_A))
	var result: Dictionary = _recorder.stop_recording()
	assert_int(result.get("events_count")).is_equal(2)
	assert_str(str(result.get("path"))).is_equal(path)
	assert_int(result.get("duration_ms")).is_greater_equal(0)
	assert_bool(FileAccess.file_exists(path)).is_true()

	# Round-trip through the reader rather than asserting on raw JSON types:
	# Godot's JSON parser hands back every number as a float, so `version` and
	# `t_ms` come out as 1.0 / 250.0 and only the reader's coercion makes them
	# ints again. That coercion is the contract worth pinning.
	var doc: Dictionary = _read_json(path)
	assert_str(str(doc.get("session_id"))).is_equal(str(result.get("session_id")))
	var reread: Dictionary = RECORDER.parse_recording(doc)
	assert_bool(reread.has("error")).is_false()
	assert_array(reread.get("events")).has_size(2)


func test_parse_recording_accepts_json_float_numbers() -> void:
	# What JSON.parse actually produces for {"version": 1, "t_ms": 250}.
	var result: Dictionary = RECORDER.parse_recording({
		"version": 1.0,
		"events": [{"type": "key", "keycode": 87.0, "t_ms": 250.0}],
	})
	assert_bool(result.has("error")).is_false()
	var events: Array = result.get("events", [])
	assert_array(events).has_size(1)
	var first: Dictionary = events[0]
	assert_int(first.get("t_ms")).is_equal(250)


# --- replay ------------------------------------------------------------------


func test_replay_reports_event_count_and_duration() -> void:
	var path: String = _tmp_path("replay.json")
	_write_json(path, RECORDER.build_recording(
		[{"type": "key", "keycode": KEY_W, "pressed": true, "t_ms": 0}],
		"sess-1", "", 0))
	var result: Dictionary = await _recorder.start_replay(path, 1.0, false)
	assert_bool(result.get("replayed")).is_true()
	assert_int(result.get("events_count")).is_equal(1)
	assert_int(result.get("duration_ms")).is_greater_equal(0)


func test_replay_speed_multiplier_shortens_the_run() -> void:
	var path: String = _tmp_path("timed.json")
	_write_json(path, RECORDER.build_recording(
		[
			{"type": "key", "keycode": KEY_W, "pressed": true, "t_ms": 0},
			{"type": "key", "keycode": KEY_W, "pressed": false, "t_ms": 600},
		],
		"sess-1", "", 600))
	var fast: Dictionary = await _recorder.start_replay(path, 6.0, false)
	assert_int(fast.get("events_count")).is_equal(2)
	# 600 ms of recording at 6x is ~100 ms; a realtime replay could not finish
	# inside 400 ms, so this fails if the multiplier is ignored.
	assert_int(fast.get("duration_ms")).is_less(400)


func test_replay_of_a_missing_file_is_an_error() -> void:
	var result: Dictionary = await _recorder.start_replay(_tmp_path("nope.json"), 1.0, false)
	assert_str(str(result.get("error", ""))).is_not_empty()


func test_replay_while_recording_is_refused() -> void:
	var path: String = _tmp_path("run.json")
	var _start: Dictionary = _recorder.start_recording(path, false)
	var result: Dictionary = await _recorder.start_replay(path, 1.0, false)
	assert_str(str(result.get("error", ""))).is_not_empty()


# --- helpers -----------------------------------------------------------------


func _key_event(keycode: Key) -> InputEventKey:
	var event: InputEventKey = InputEventKey.new()
	event.keycode = keycode
	event.pressed = true
	return event


func _motion_event() -> InputEventMouseMotion:
	var event: InputEventMouseMotion = InputEventMouseMotion.new()
	event.position = Vector2(5, 5)
	event.relative = Vector2(1, 1)
	return event


func _write_json(path: String, data: Dictionary) -> void:
	var file: FileAccess = FileAccess.open(path, FileAccess.WRITE)
	assert_object(file).is_not_null()
	file.store_string(JSON.stringify(data))
	file.close()


func _read_json(path: String) -> Dictionary:
	var text: String = FileAccess.get_file_as_string(path)
	var json: JSON = JSON.new()
	assert_int(json.parse(text)).is_equal(OK)
	var parsed: Variant = json.data
	assert_bool(parsed is Dictionary).is_true()
	return parsed
