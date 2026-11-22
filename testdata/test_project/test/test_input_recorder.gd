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
const ERRORS := preload("res://addons/stagehand/core/errors.gd")

## Scratch space inside the recording sandbox: the recorder refuses any path
## outside user://stagehand_recordings/, so GdUnit's own temp dir is off limits.
const TMP_DIR: String = "user://stagehand_recordings/gdunit_input_recorder"

var _recorder: RECORDER
var _tmp_dir: String


func before_test() -> void:
	_recorder = RECORDER.new()
	add_child(_recorder)
	auto_free(_recorder)
	_tmp_dir = TMP_DIR
	DirAccess.make_dir_recursive_absolute(_tmp_dir)


func after_test() -> void:
	# Stop first so _exit_tree does not write a file after the sweep below.
	if _recorder.is_recording():
		var _stopped: Dictionary = _recorder.stop_recording()
	for file_name: String in DirAccess.get_files_at(_tmp_dir):
		DirAccess.remove_absolute("%s/%s" % [_tmp_dir, file_name])


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


func test_serialize_key_event_captures_physical_keycode_and_key_label() -> void:
	var event: InputEventKey = InputEventKey.new()
	event.keycode = KEY_W
	event.physical_keycode = KEY_Z
	event.key_label = KEY_Q
	var out: Dictionary = RECORDER.serialize_event(event)
	assert_int(out.get("physical_keycode")).is_equal(KEY_Z)
	assert_int(out.get("key_label")).is_equal(KEY_Q)


func test_round_trip_preserves_physical_keycode_and_key_label() -> void:
	var event: InputEventKey = InputEventKey.new()
	event.keycode = KEY_W
	event.physical_keycode = KEY_Z
	event.key_label = KEY_Q
	event.pressed = true
	var restored: InputEvent = RECORDER.deserialize_event(RECORDER.serialize_event(event))
	var key_event: InputEventKey = restored
	assert_int(key_event.physical_keycode).is_equal(KEY_Z)
	assert_int(key_event.key_label).is_equal(KEY_Q)


func test_deserialize_legacy_key_entry_without_physical_fields() -> void:
	var restored: InputEvent = RECORDER.deserialize_event(
		{"type": "key", "keycode": KEY_A, "pressed": true}
	)
	var key_event: InputEventKey = restored
	assert_int(key_event.keycode).is_equal(KEY_A)
	assert_int(key_event.physical_keycode).is_equal(0)
	assert_int(key_event.key_label).is_equal(0)


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
	_recorder.capture(_motion_event())
	_recorder.capture(_key_event(KEY_W))
	assert_int(_recorder.pending_event_count()).is_equal(1)


func test_mouse_motion_is_captured_when_requested() -> void:
	var _start: Dictionary = _recorder.start_recording(_tmp_path("run.json"), true)
	_recorder.capture(_motion_event())
	_recorder.capture(_key_event(KEY_W))
	assert_int(_recorder.pending_event_count()).is_equal(2)


func test_events_outside_a_recording_are_ignored() -> void:
	_recorder.capture(_key_event(KEY_W))
	assert_int(_recorder.pending_event_count()).is_equal(0)


func test_stop_recording_reports_counts_and_writes_the_file() -> void:
	var path: String = _tmp_path("run.json")
	var _start: Dictionary = _recorder.start_recording(path, false)
	_recorder.capture(_key_event(KEY_W))
	_recorder.capture(_key_event(KEY_A))
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


# --- path sandbox ------------------------------------------------------------


func test_resolve_recording_path_keeps_paths_inside_the_recording_dir() -> void:
	var cases: Dictionary = {
		"ok.json": "user://stagehand_recordings/ok.json",
		"runs/menu_flow-2.json": "user://stagehand_recordings/runs/menu_flow-2.json",
		"user://stagehand_recordings/ok.json": "user://stagehand_recordings/ok.json",
		"user://stagehand_recordings/a/b.json": "user://stagehand_recordings/a/b.json",
	}
	for input: String in cases:
		var resolved: Dictionary = RECORDER.resolve_recording_path(input)
		assert_bool(ERRORS.is_error(resolved)).override_failure_message("rejected %s" % input).is_false()
		assert_str(str(resolved.get("path"))).is_equal(str(cases[input]))


func test_resolve_recording_path_rejects_escapes() -> void:
	for input: String in [
		"", "res://x.json", "res://player.gd", "user://x.json", "user://../x.json",
		"user://stagehand_recordings", "user://stagehand_recordings/",
		"user://stagehand_recordings/../x.json", "user://stagehand_recordings_evil/x.json",
		"/tmp/x.json", "C:/Users/x.json", "C:\\Users\\x.json", "sub\\x.json", "../x.json",
		"a/../../x.json", "./x.json", "a//x.json", "x.json/", "file:///tmp/x.json", "x y.json",
	]:
		var resolved: Dictionary = RECORDER.resolve_recording_path(input)
		assert_str(ERRORS.code_of(resolved)).override_failure_message("accepted %s" % input) \
			.is_equal(ERRORS.INVALID_PARAMS)


## Plants a symlink at [param link_path] pointing at [param target]. Godot has
## no symlink API, so this shells out; returns false where `ln` is unavailable.
func _make_symlink(link_path: String, target: String) -> bool:
	var exit_code: int = OS.execute("ln", ["-s", target, ProjectSettings.globalize_path(link_path)])
	return exit_code == 0


## Godot's remove_absolute cannot unlink a symlink to a directory.
func _remove_symlink(link_path: String) -> void:
	var _exit_code: int = OS.execute("rm", ["-f", ProjectSettings.globalize_path(link_path)])


func test_symlinked_directory_inside_the_recording_dir_is_refused() -> void:
	if OS.get_name() == "Windows":
		return
	var outside: String = OS.get_temp_dir().path_join("stagehand_symlink_outside_dir")
	DirAccess.make_dir_recursive_absolute(outside)
	var link: String = _tmp_path("linked_dir")
	_remove_symlink(link)
	assert_bool(_make_symlink(link, outside)).is_true()
	for op: String in ["record", "replay"]:
		var result: Dictionary
		if op == "record":
			result = _recorder.start_recording("gdunit_input_recorder/linked_dir/x.json", false)
		else:
			result = await _recorder.start_replay("gdunit_input_recorder/linked_dir/x.json", 1.0, false)
		assert_str(ERRORS.code_of(result)).override_failure_message("%s followed a symlinked dir" % op) \
			.is_equal(ERRORS.INVALID_PARAMS)
	assert_bool(_recorder.is_recording()).is_false()
	_remove_symlink(link)
	DirAccess.remove_absolute(outside)


func test_symlinked_file_inside_the_recording_dir_is_refused() -> void:
	if OS.get_name() == "Windows":
		return
	var outside: String = OS.get_temp_dir().path_join("stagehand_symlink_outside.json")
	var seed_file: FileAccess = FileAccess.open(outside, FileAccess.WRITE)
	seed_file.store_string("{}")
	seed_file.close()
	var link: String = _tmp_path("linked.json")
	assert_bool(_make_symlink(link, outside)).is_true()
	var result: Dictionary = _recorder.start_recording("gdunit_input_recorder/linked.json", false)
	assert_str(ERRORS.code_of(result)).is_equal(ERRORS.INVALID_PARAMS)
	assert_bool(_recorder.is_recording()).is_false()
	_remove_symlink(link)
	DirAccess.remove_absolute(outside)


func test_allow_unsafe_paths_skips_the_symlink_check() -> void:
	if OS.get_name() == "Windows":
		return
	var outside: String = OS.get_temp_dir().path_join("stagehand_symlink_unsafe.json")
	var link: String = _tmp_path("unsafe_linked.json")
	assert_bool(_make_symlink(link, outside)).is_true()
	_recorder.allow_unsafe_paths = true
	var result: Dictionary = _recorder.start_recording(link, false)
	assert_bool(ERRORS.is_error(result)).is_false()
	var _stopped: Dictionary = _recorder.stop_recording()
	_remove_symlink(link)
	DirAccess.remove_absolute(outside)


func test_start_recording_rejects_paths_outside_the_recording_dir() -> void:
	for input: String in ["res://x.json", "user://../x.json", "/tmp/x.json"]:
		var result: Dictionary = _recorder.start_recording(input, false)
		assert_str(ERRORS.code_of(result)).override_failure_message("accepted %s" % input) \
			.is_equal(ERRORS.INVALID_PARAMS)
		assert_bool(_recorder.is_recording()).is_false()


func test_start_recording_accepts_paths_inside_the_recording_dir() -> void:
	for input: String in [_tmp_path("ok.json"), "gdunit_input_recorder/ok.json"]:
		var result: Dictionary = _recorder.start_recording(input, false)
		assert_bool(ERRORS.is_error(result)).override_failure_message("rejected %s" % input).is_false()
		assert_str(str(result.get("output_path"))).is_equal(_tmp_path("ok.json"))
		var stopped: Dictionary = _recorder.stop_recording()
		assert_str(str(stopped.get("path"))).is_equal(_tmp_path("ok.json"))
		assert_bool(FileAccess.file_exists(_tmp_path("ok.json"))).is_true()


func test_replayed_click_hits_the_same_control_under_content_scale() -> void:
	var root: Window = get_tree().root
	var saved_size: Vector2i = root.size
	var saved_scale: float = root.content_scale_factor
	root.size = Vector2i(1152, 648)
	root.content_scale_factor = 2.0
	var button: Button = auto_free(Button.new())
	button.position = Vector2(300.0, 200.0)
	button.size = Vector2(120.0, 40.0)
	button.add_to_group(&"recorder_stretch_target")
	var presses: Array[int] = [0]
	button.pressed.connect(func() -> void: presses[0] += 1)
	add_child(button)
	await get_tree().process_frame

	var path: String = _tmp_path("stretch.json")
	var _start: Dictionary = _recorder.start_recording(path, false)
	var clicked: Dictionary = StagehandInputSimulator.input_mouse(
		get_tree(), {"selector": "group:recorder_stretch_target", "hold_ms": 0}
	)
	assert_bool(clicked.get("success", false)).is_true()
	await get_tree().process_frame
	await get_tree().process_frame
	var stopped: Dictionary = _recorder.stop_recording()
	assert_int(presses[0]).override_failure_message("live click missed").is_equal(1)
	assert_int(stopped.get("events_count")).is_greater_equal(2)

	presses[0] = 0
	var replayed: Dictionary = await _recorder.start_replay(path, 1.0, false)
	await get_tree().process_frame
	await get_tree().process_frame
	root.content_scale_factor = saved_scale
	root.size = saved_size
	assert_bool(replayed.get("replayed", false)).is_true()
	assert_int(presses[0]).override_failure_message("replayed click missed the button").is_equal(1)


func test_recorder_captures_input_a_game_node_consumes_in_its_input() -> void:
	var consumer: _InputConsumer = auto_free(_InputConsumer.new())
	get_tree().root.add_child(consumer)
	var _start: Dictionary = _recorder.start_recording(_tmp_path("consumed.json"), false)
	await get_tree().process_frame
	get_tree().root.push_input(_key_event(KEY_ESCAPE))
	assert_int(consumer.seen).override_failure_message("consumer never saw the key").is_equal(1)
	assert_int(_recorder.pending_event_count()).is_equal(1)


func test_recorder_keeps_capturing_first_after_a_node_joins_the_root() -> void:
	var _start: Dictionary = _recorder.start_recording(_tmp_path("late.json"), false)
	var consumer: _InputConsumer = auto_free(_InputConsumer.new())
	get_tree().root.add_child(consumer)
	await get_tree().process_frame
	get_tree().root.push_input(_key_event(KEY_ESCAPE))
	assert_int(consumer.seen).is_equal(1)
	assert_int(_recorder.pending_event_count()).is_equal(1)


func test_recorder_captures_input_while_the_tree_is_paused() -> void:
	# The pause-menu case: the tree is paused and an always-processing menu
	# swallows the key. Pushed and unpaused without yielding a frame, so the
	# test runner itself never runs paused.
	var consumer: _InputConsumer = auto_free(_InputConsumer.new())
	consumer.process_mode = Node.PROCESS_MODE_ALWAYS
	get_tree().root.add_child(consumer)
	var _start: Dictionary = _recorder.start_recording(_tmp_path("paused.json"), false)
	get_tree().paused = true
	get_tree().root.push_input(_key_event(KEY_ESCAPE))
	get_tree().paused = false
	assert_int(consumer.seen).is_equal(1)
	assert_int(_recorder.pending_event_count()).is_equal(1)


func test_serialize_key_event_records_echo() -> void:
	var event: InputEventKey = _key_event(KEY_DOWN)
	event.echo = true
	var out: Dictionary = RECORDER.serialize_event(event)
	assert_bool(out.get("echo", false)).is_true()


func test_round_trip_preserves_key_echo() -> void:
	var event: InputEventKey = _key_event(KEY_DOWN)
	event.echo = true
	var restored: InputEventKey = RECORDER.deserialize_event(RECORDER.serialize_event(event))
	assert_bool(restored.echo).is_true()


func test_deserialize_legacy_key_entry_without_echo_is_not_an_echo() -> void:
	var restored: InputEventKey = RECORDER.deserialize_event(
		{"type": "key", "keycode": KEY_DOWN, "pressed": true}
	)
	assert_bool(restored.echo).is_false()


func test_replay_rejects_paths_outside_the_recording_dir() -> void:
	# Real, readable files: the refusal must come before the open, so replay is
	# not an existence/JSON oracle for files outside the sandbox.
	for input: String in ["res://project.godot", "user://../x.json", "/etc/hostname"]:
		var result: Dictionary = await _recorder.start_replay(input, 1.0, false)
		assert_str(ERRORS.code_of(result)).override_failure_message("accepted %s" % input) \
			.is_equal(ERRORS.INVALID_PARAMS)


func test_unsafe_mode_lifts_the_path_sandbox() -> void:
	_recorder.allow_unsafe_paths = true
	var path: String = "user://gdunit_input_recorder_unsafe.json"
	var result: Dictionary = _recorder.start_recording(path, false)
	assert_str(str(result.get("output_path"))).is_equal(path)
	var stopped: Dictionary = _recorder.stop_recording()
	assert_bool(ERRORS.is_error(stopped)).is_false()
	DirAccess.remove_absolute(path)


# --- helpers -----------------------------------------------------------------


## A game node that swallows every event in [method Node._input], the way a
## pause menu does.
class _InputConsumer extends Node:
	var seen: int = 0

	func _input(_event: InputEvent) -> void:
		seen += 1
		get_viewport().set_input_as_handled()



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
