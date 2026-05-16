class_name StagehandInputRecorder
extends Node
## Records input events with timestamps and replays them from saved files.
## Used by the Stagehand server to implement record/replay automation.

var _recording: bool = false
var _output_path: String = ""
var _frames: Array[Dictionary] = []
var _start_time_ms: int = 0


func _input(event: InputEvent) -> void:
	if not _recording:
		return
	var serialized: Dictionary = _serialize_event(event)
	if serialized.is_empty():
		return
	serialized["time_ms"] = Time.get_ticks_msec() - _start_time_ms
	_frames.append(serialized)


func _exit_tree() -> void:
	if _recording:
		stop_recording()


func start_recording(output_path: String) -> Dictionary:
	if _recording:
		return {"error": "Already recording"}
	_output_path = output_path
	_frames = []
	_start_time_ms = Time.get_ticks_msec()
	_recording = true
	return {"success": true, "output_path": output_path}


func stop_recording() -> Dictionary:
	if not _recording:
		return {"error": "Not recording"}
	_recording = false
	var frame_count: int = _frames.size()
	var data: Dictionary = {
		"version": 1,
		"frames": _frames,
	}
	var json_text: String = JSON.stringify(data, "\t")
	var file := FileAccess.open(_output_path, FileAccess.WRITE)
	if file == null:
		var err: int = FileAccess.get_open_error()
		return {"error": "Failed to open file for writing: %s (%s)" % [_output_path, error_string(err)]}
	file.store_string(json_text)
	_frames = []
	return {"success": true, "frames": frame_count}


func start_replay(input_path: String) -> Dictionary:
	if _recording:
		return {"error": "Cannot replay while recording"}
	var file := FileAccess.open(input_path, FileAccess.READ)
	if file == null:
		var err: int = FileAccess.get_open_error()
		return {"error": "Failed to open file for reading: %s (%s)" % [input_path, error_string(err)]}
	var text: String = file.get_as_text()
	var json := JSON.new()
	var parse_err := json.parse(text)
	if parse_err != OK:
		return {"error": "Failed to parse recording: %s" % json.get_error_message()}
	var data: Variant = json.data
	if data is not Dictionary:
		return {"error": "Invalid recording format"}
	var frames: Array = data.get("frames", [])
	if frames.is_empty():
		return {"success": true, "input_path": input_path, "frames": 0}

	var last_delay_sec: float = 0.0
	var replayed_count: int = 0
	for frame_variant: Variant in frames:
		if frame_variant is not Dictionary:
			continue
		var frame: Dictionary = frame_variant
		var event: InputEvent = _deserialize_event(frame)
		if event == null:
			continue
		replayed_count += 1
		var delay_sec: float = int(frame.get("time_ms", 0)) / 1000.0
		if delay_sec > last_delay_sec:
			last_delay_sec = delay_sec
		if delay_sec <= 0.0:
			Input.parse_input_event(event)
		else:
			get_tree().create_timer(delay_sec).timeout.connect(func() -> void:
				Input.parse_input_event(event)
			)

	if last_delay_sec > 0.0:
		await get_tree().create_timer(last_delay_sec + 0.05).timeout

	return {"success": true, "input_path": input_path, "frames": replayed_count}


static func _serialize_event(event: InputEvent) -> Dictionary:
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


static func _deserialize_event(frame: Dictionary) -> InputEvent:
	var type: String = frame.get("type", "")
	match type:
		"key":
			var ev := InputEventKey.new()
			ev.keycode = int(frame.get("keycode", 0))
			ev.unicode = int(frame.get("unicode", 0))
			ev.pressed = bool(frame.get("pressed", false))
			ev.shift_pressed = bool(frame.get("shift", false))
			ev.ctrl_pressed = bool(frame.get("ctrl", false))
			ev.alt_pressed = bool(frame.get("alt", false))
			ev.meta_pressed = bool(frame.get("meta", false))
			return ev
		"mouse_button":
			var ev := InputEventMouseButton.new()
			var pos: Dictionary = frame.get("position", {})
			ev.position = Vector2(float(pos.get("x", 0.0)), float(pos.get("y", 0.0)))
			ev.global_position = ev.position
			ev.button_index = int(frame.get("button_index", MOUSE_BUTTON_LEFT))
			ev.pressed = bool(frame.get("pressed", false))
			ev.double_click = bool(frame.get("double_click", false))
			return ev
		"mouse_motion":
			var ev := InputEventMouseMotion.new()
			var pos: Dictionary = frame.get("position", {})
			ev.position = Vector2(float(pos.get("x", 0.0)), float(pos.get("y", 0.0)))
			ev.global_position = ev.position
			var rel: Dictionary = frame.get("relative", {})
			ev.relative = Vector2(float(rel.get("x", 0.0)), float(rel.get("y", 0.0)))
			return ev
		"action":
			var ev := InputEventAction.new()
			ev.action = str(frame.get("action", ""))
			ev.strength = float(frame.get("strength", 1.0))
			ev.pressed = bool(frame.get("pressed", false))
			return ev
		"screen_touch":
			var ev := InputEventScreenTouch.new()
			var pos: Dictionary = frame.get("position", {})
			ev.position = Vector2(float(pos.get("x", 0.0)), float(pos.get("y", 0.0)))
			ev.index = int(frame.get("index", 0))
			ev.pressed = bool(frame.get("pressed", false))
			return ev
		"screen_drag":
			var ev := InputEventScreenDrag.new()
			var pos: Dictionary = frame.get("position", {})
			ev.position = Vector2(float(pos.get("x", 0.0)), float(pos.get("y", 0.0)))
			ev.index = int(frame.get("index", 0))
			var rel: Dictionary = frame.get("relative", {})
			ev.relative = Vector2(float(rel.get("x", 0.0)), float(rel.get("y", 0.0)))
			return ev
	return null
