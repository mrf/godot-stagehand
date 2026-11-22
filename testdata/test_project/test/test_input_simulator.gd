# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for StagehandInputSimulator — that each command synthesizes the
## correct InputEvent subclass with the right fields, and that malformed
## params are rejected before any event is pushed.
##
## Mouse and touch events are delivered synchronously via
## Viewport.push_input, so a capture node in the tree observes them within the
## same call. Key, action, and text events go through Input.parse_input_event,
## which the engine dispatches on the next frame, so those tests await one.


## Records every InputEvent the viewport dispatches, for later inspection.
class EventRecorder:
	extends Node

	var events: Array[InputEvent] = []

	func _input(event: InputEvent) -> void:
		events.append(event)

	func of_type(type_name: String) -> Array[InputEvent]:
		var matches: Array[InputEvent] = []
		for event: InputEvent in events:
			if event.is_class(type_name):
				matches.append(event)
		return matches

	func first_of_type(type_name: String) -> InputEvent:
		var matches: Array[InputEvent] = of_type(type_name)
		return matches[0] if not matches.is_empty() else null


const GROUP: StringName = &"input_sim_target"
## Longer than any hold/duration a test leaves pending at its end.
const SIMULATOR_SETTLE_SEC: float = 0.15

var _recorder: EventRecorder
var _button: Button


func before_test() -> void:
	_recorder = auto_free(EventRecorder.new())
	_recorder.name = "InputRecorderProbe"
	add_child(_recorder)

	_button = auto_free(Button.new())
	_button.name = "InputSimButton"
	_button.text = "Press Me"
	_button.size = Vector2(80.0, 30.0)
	_button.position = Vector2(400.0, 300.0)
	_button.add_to_group(GROUP)
	add_child(_button)


func _at(x: float, y: float) -> Dictionary:
	return {"x": x, "y": y}


# ── input_mouse: event synthesis ─────────────────────────────────────────

func test_mouse_click_pushes_mouse_button_event() -> void:
	StagehandInputSimulator.input_mouse(get_tree(), {"position": _at(120.0, 140.0)})

	var event: InputEventMouseButton = _recorder.first_of_type("InputEventMouseButton")
	assert_object(event).is_not_null()
	assert_bool(event.pressed).is_true()
	assert_int(event.button_index).is_equal(MOUSE_BUTTON_LEFT)
	assert_vector(event.position).is_equal(Vector2(120.0, 140.0))


## A real pointer always moves before it clicks, so a position-only click must
## push a leading motion event too, not just for selector-driven clicks.
func test_position_click_is_preceded_by_a_motion_event() -> void:
	StagehandInputSimulator.input_mouse(get_tree(), {"position": _at(50.0, 60.0)})
	assert_int(_recorder.of_type("InputEventMouseButton").size()).is_greater_equal(1)
	assert_int(_recorder.of_type("InputEventMouseMotion").size()).is_greater_equal(1)


func test_selector_click_is_preceded_by_a_motion_event() -> void:
	# A real pointer always moves before it clicks; the leading motion event is
	# what makes hover-dependent Controls respond.
	StagehandInputSimulator.input_mouse(get_tree(), {"selector": "group:%s" % GROUP})
	assert_int(_recorder.of_type("InputEventMouseMotion").size()).is_greater_equal(1)


func test_mouse_right_button_is_mapped() -> void:
	StagehandInputSimulator.input_mouse(
		get_tree(), {"position": _at(10.0, 10.0), "button": "right"}
	)
	var event: InputEventMouseButton = _recorder.first_of_type("InputEventMouseButton")
	assert_int(event.button_index).is_equal(MOUSE_BUTTON_RIGHT)


func test_mouse_middle_button_is_mapped() -> void:
	StagehandInputSimulator.input_mouse(
		get_tree(), {"position": _at(10.0, 10.0), "button": "middle"}
	)
	var event: InputEventMouseButton = _recorder.first_of_type("InputEventMouseButton")
	assert_int(event.button_index).is_equal(MOUSE_BUTTON_MIDDLE)


func test_mouse_unknown_button_falls_back_to_left() -> void:
	StagehandInputSimulator.input_mouse(
		get_tree(), {"position": _at(10.0, 10.0), "button": "nonsense"}
	)
	var event: InputEventMouseButton = _recorder.first_of_type("InputEventMouseButton")
	assert_int(event.button_index).is_equal(MOUSE_BUTTON_LEFT)


func test_mouse_double_click_flag_is_set() -> void:
	StagehandInputSimulator.input_mouse(
		get_tree(), {"position": _at(10.0, 10.0), "double_click": true}
	)
	var event: InputEventMouseButton = _recorder.first_of_type("InputEventMouseButton")
	assert_bool(event.double_click).is_true()


func test_mouse_reports_clicked_position() -> void:
	var result: Dictionary = StagehandInputSimulator.input_mouse(
		get_tree(), {"position": _at(11.0, 22.0)}
	)
	assert_bool(result.get("success", false)).is_true()
	var clicked: Dictionary = result.get("clicked_at", {})
	assert_float(clicked.get("x", 0.0)).is_equal_approx(11.0, 0.001)
	assert_float(clicked.get("y", 0.0)).is_equal_approx(22.0, 0.001)


# ── input_mouse: error cases ─────────────────────────────────────────────

func test_mouse_without_selector_or_position_returns_error() -> void:
	var result: Dictionary = StagehandInputSimulator.input_mouse(get_tree(), {})
	assert_str(str(result.get("error", ""))).contains("Missing selector or position")
	assert_int(_recorder.events.size()).is_equal(0)


func test_mouse_unmatched_selector_returns_error() -> void:
	var result: Dictionary = StagehandInputSimulator.input_mouse(
		get_tree(), {"selector": "group:no_such_group_at_all"}
	)
	assert_str(str(result.get("error", ""))).contains("Node not found")


func test_mouse_non_canvas_node_returns_error() -> void:
	var plain: Node = auto_free(Node.new())
	plain.name = "NotClickable"
	plain.add_to_group(&"input_sim_plain_node")
	add_child(plain)

	var result: Dictionary = StagehandInputSimulator.input_mouse(
		get_tree(), {"selector": "group:input_sim_plain_node"}
	)
	assert_str(str(result.get("error", ""))).contains("does not support clicking")


# ── input_touch: event synthesis ─────────────────────────────────────────

func test_touch_tap_pushes_screen_touch_event() -> void:
	StagehandInputSimulator.input_touch(get_tree(), {"position": _at(30.0, 40.0)})

	var event: InputEventScreenTouch = _recorder.first_of_type("InputEventScreenTouch")
	assert_object(event).is_not_null()
	assert_bool(event.pressed).is_true()
	assert_vector(event.position).is_equal(Vector2(30.0, 40.0))


func test_touch_honors_finger_index() -> void:
	StagehandInputSimulator.input_touch(
		get_tree(), {"position": _at(1.0, 2.0), "index": 3}
	)
	var event: InputEventScreenTouch = _recorder.first_of_type("InputEventScreenTouch")
	assert_int(event.index).is_equal(3)


func test_touch_end_pushes_released_touch_event() -> void:
	StagehandInputSimulator.input_touch(
		get_tree(), {"position": _at(5.0, 5.0), "action": "end"}
	)
	var event: InputEventScreenTouch = _recorder.first_of_type("InputEventScreenTouch")
	assert_bool(event.pressed).is_false()


func test_touch_move_pushes_screen_drag_event() -> void:
	StagehandInputSimulator.input_touch(get_tree(), {
		"position": _at(10.0, 10.0),
		"drag_to": _at(60.0, 30.0),
		"action": "move",
	})

	var event: InputEventScreenDrag = _recorder.first_of_type("InputEventScreenDrag")
	assert_object(event).is_not_null()
	assert_vector(event.position).is_equal(Vector2(60.0, 30.0))
	# relative is the delta travelled from the start point.
	assert_vector(event.relative).is_equal(Vector2(50.0, 20.0))


func test_touch_missing_position_returns_error() -> void:
	var result: Dictionary = StagehandInputSimulator.input_touch(get_tree(), {})
	assert_str(str(result.get("error", ""))).contains("Missing required parameter: position")
	assert_int(_recorder.events.size()).is_equal(0)


func test_touch_move_without_drag_to_returns_error() -> void:
	var result: Dictionary = StagehandInputSimulator.input_touch(
		get_tree(), {"position": _at(1.0, 1.0), "action": "move"}
	)
	assert_str(str(result.get("error", ""))).contains("drag_to is required")


func test_touch_unknown_action_returns_error() -> void:
	var result: Dictionary = StagehandInputSimulator.input_touch(
		get_tree(), {"position": _at(1.0, 1.0), "action": "wiggle"}
	)
	assert_str(str(result.get("error", ""))).contains("Unknown action")


# ── input_touch: mouse emulation ─────────────────────────────────────────
# A real touchscreen tap reaches mouse-only Controls such as Button because
# Input synthesizes mouse events from touch when emulate_mouse_from_touch is on.

var _saved_mouse_from_touch: bool = Input.emulate_mouse_from_touch


func _with_mouse_from_touch(enabled: bool) -> void:
	Input.emulate_mouse_from_touch = enabled


func _count_presses(button: BaseButton) -> Array[int]:
	var count: Array[int] = [0]
	var _err: int = button.pressed.connect(func() -> void: count[0] += 1)
	return count


func test_touch_begin_end_on_a_button_emits_pressed() -> void:
	_with_mouse_from_touch(true)
	var presses: Array[int] = _count_presses(_button)

	StagehandInputSimulator.input_touch(get_tree(), {"position": _at(440.0, 315.0), "action": "begin"})
	StagehandInputSimulator.input_touch(get_tree(), {"position": _at(440.0, 315.0), "action": "end"})

	assert_int(presses[0]).is_equal(1)


func test_touch_tap_on_a_button_emits_pressed() -> void:
	_with_mouse_from_touch(true)
	var presses: Array[int] = _count_presses(_button)

	StagehandInputSimulator.input_touch(get_tree(), {"position": _at(440.0, 315.0), "duration_ms": 20})
	await get_tree().create_timer(0.15, true, false, true).timeout

	assert_int(presses[0]).is_equal(1)


func test_touch_emulates_left_mouse_button_events() -> void:
	_with_mouse_from_touch(true)
	StagehandInputSimulator.input_touch(get_tree(), {"position": _at(30.0, 40.0), "action": "begin"})

	var event: InputEventMouseButton = _recorder.first_of_type("InputEventMouseButton")
	assert_object(event).is_not_null()
	assert_bool(event.pressed).is_true()
	assert_int(event.button_index).is_equal(MOUSE_BUTTON_LEFT)
	assert_vector(event.position).is_equal(Vector2(30.0, 40.0))


func test_touch_drag_emulates_mouse_motion_with_left_button_held() -> void:
	_with_mouse_from_touch(true)
	StagehandInputSimulator.input_touch(get_tree(), {
		"position": _at(10.0, 10.0),
		"drag_to": _at(60.0, 30.0),
		"action": "move",
	})

	var event: InputEventMouseMotion = _recorder.first_of_type("InputEventMouseMotion")
	assert_object(event).is_not_null()
	assert_vector(event.position).is_equal(Vector2(60.0, 30.0))
	assert_vector(event.relative).is_equal(Vector2(50.0, 20.0))
	assert_int(event.button_mask).is_equal(MOUSE_BUTTON_MASK_LEFT)


## Godot emulates the mouse from the first finger only; a second finger must
## not click on its own.
func test_touch_from_a_secondary_finger_emulates_no_mouse() -> void:
	_with_mouse_from_touch(true)
	StagehandInputSimulator.input_touch(
		get_tree(), {"position": _at(30.0, 40.0), "action": "begin", "index": 1}
	)
	assert_int(_recorder.of_type("InputEventMouseButton").size()).is_equal(0)


func test_touch_without_mouse_emulation_pushes_no_mouse_events() -> void:
	_with_mouse_from_touch(false)
	var presses: Array[int] = _count_presses(_button)

	StagehandInputSimulator.input_touch(get_tree(), {"position": _at(440.0, 315.0), "action": "begin"})
	StagehandInputSimulator.input_touch(get_tree(), {"position": _at(440.0, 315.0), "action": "end"})

	assert_int(_recorder.of_type("InputEventMouseButton").size()).is_equal(0)
	assert_int(presses[0]).is_equal(0)


# ── input_mouse: polled input state ──────────────────────────────────────
# Pointer events are delivered with Viewport.push_input, which Input never
# sees. Actions bound to the button are kept in sync explicitly; the raw
# button mask is not (documented in docs/tools.md).

const MOUSE_ACTION: StringName = &"stagehand_test_mouse_fire"


func _add_mouse_action(button: MouseButton) -> void:
	var bind: InputEventMouseButton = InputEventMouseButton.new()
	bind.button_index = button
	# -1 is "all devices", what the editor's Input Map writes; it is also what
	# lets the emulated mouse (device DEVICE_ID_EMULATION) match.
	bind.device = -1
	InputMap.add_action(MOUSE_ACTION)
	InputMap.action_add_event(MOUSE_ACTION, bind)


func test_mouse_click_holds_a_mouse_bound_action_until_release() -> void:
	_add_mouse_action(MOUSE_BUTTON_LEFT)
	StagehandInputSimulator.input_mouse(get_tree(), {"position": _at(10.0, 10.0), "hold_ms": 50})

	assert_bool(Input.is_action_pressed(MOUSE_ACTION)).is_true()
	await get_tree().create_timer(0.2, true, false, true).timeout
	assert_bool(Input.is_action_pressed(MOUSE_ACTION)).is_false()


func test_mouse_click_with_another_button_leaves_the_action_alone() -> void:
	_add_mouse_action(MOUSE_BUTTON_LEFT)
	StagehandInputSimulator.input_mouse(
		get_tree(), {"position": _at(10.0, 10.0), "button": "right", "hold_ms": 50}
	)
	assert_bool(Input.is_action_pressed(MOUSE_ACTION)).is_false()


func test_touch_holds_a_mouse_bound_action_via_emulation() -> void:
	_with_mouse_from_touch(true)
	_add_mouse_action(MOUSE_BUTTON_LEFT)

	StagehandInputSimulator.input_touch(get_tree(), {"position": _at(10.0, 10.0), "action": "begin"})
	assert_bool(Input.is_action_pressed(MOUSE_ACTION)).is_true()
	StagehandInputSimulator.input_touch(get_tree(), {"position": _at(10.0, 10.0), "action": "end"})
	assert_bool(Input.is_action_pressed(MOUSE_ACTION)).is_false()


## Documented limitation: Input exposes no way to set its button mask without
## also dispatching the event a second time through the window.
func test_mouse_click_does_not_set_polled_mouse_button_state() -> void:
	StagehandInputSimulator.input_mouse(get_tree(), {"position": _at(10.0, 10.0), "hold_ms": 50})
	assert_bool(Input.is_mouse_button_pressed(MOUSE_BUTTON_LEFT)).is_false()


func test_mouse_release_after_the_viewport_is_freed_still_releases_the_action() -> void:
	_add_mouse_action(MOUSE_BUTTON_LEFT)
	var viewport: SubViewport = SubViewport.new()
	add_child(viewport)
	StagehandInputSimulator._push_mouse_button(viewport, Vector2.ZERO, MOUSE_BUTTON_LEFT, true, false)
	StagehandInputSimulator._release_mouse_after(
		get_tree(), viewport, Vector2.ZERO, MOUSE_BUTTON_LEFT, 0.05
	)
	viewport.free()
	await get_tree().create_timer(0.15, true, false, true).timeout
	assert_bool(Input.is_action_pressed(MOUSE_ACTION)).is_false()


# ── input_mouse_move ─────────────────────────────────────────────────────

func test_mouse_move_pushes_motion_event_at_coordinates() -> void:
	var result: Dictionary = StagehandInputSimulator.input_mouse_move(
		get_tree(), {"coordinates": _at(75.0, 85.0)}
	)
	assert_bool(result.get("success", false)).is_true()
	assert_str(str(result.get("mode"))).is_equal("absolute")

	var event: InputEventMouseMotion = _recorder.first_of_type("InputEventMouseMotion")
	assert_object(event).is_not_null()
	assert_vector(event.position).is_equal(Vector2(75.0, 85.0))


func test_mouse_move_pushes_no_button_event() -> void:
	StagehandInputSimulator.input_mouse_move(get_tree(), {"coordinates": _at(5.0, 5.0)})
	assert_int(_recorder.of_type("InputEventMouseButton").size()).is_equal(0)


func test_mouse_move_by_selector_reports_mode() -> void:
	var result: Dictionary = StagehandInputSimulator.input_mouse_move(
		get_tree(), {"selector": "group:%s" % GROUP}
	)
	assert_bool(result.get("success", false)).is_true()
	assert_str(str(result.get("mode"))).is_equal("by_selector")


func test_mouse_move_without_target_returns_error() -> void:
	var result: Dictionary = StagehandInputSimulator.input_mouse_move(get_tree(), {})
	assert_str(str(result.get("error", ""))).contains("Either selector or coordinates")


func test_mouse_move_unmatched_selector_returns_error() -> void:
	var result: Dictionary = StagehandInputSimulator.input_mouse_move(
		get_tree(), {"selector": "group:no_such_group_at_all"}
	)
	assert_str(str(result.get("error", ""))).contains("Node not found")


# ── input_key ────────────────────────────────────────────────────────────

func test_key_press_pushes_key_event_with_keycode() -> void:
	StagehandInputSimulator.input_key(get_tree(), {"key": "A"})
	await get_tree().process_frame

	var event: InputEventKey = _recorder.first_of_type("InputEventKey")
	assert_object(event).is_not_null()
	assert_int(event.keycode).is_equal(KEY_A)
	assert_bool(event.pressed).is_true()


func test_key_press_applies_modifiers() -> void:
	StagehandInputSimulator.input_key(
		get_tree(), {"key": "S", "modifiers": ["ctrl", "shift"]}
	)
	await get_tree().process_frame

	var event: InputEventKey = _recorder.first_of_type("InputEventKey")
	assert_object(event).is_not_null()
	assert_bool(event.ctrl_pressed).is_true()
	assert_bool(event.shift_pressed).is_true()
	assert_bool(event.alt_pressed).is_false()


func test_key_missing_key_returns_error() -> void:
	var result: Dictionary = StagehandInputSimulator.input_key(get_tree(), {})
	assert_str(str(result.get("error", ""))).contains("Missing required parameter: key")


func test_key_unknown_key_name_returns_error() -> void:
	var result: Dictionary = StagehandInputSimulator.input_key(
		get_tree(), {"key": "NotARealKeyName"}
	)
	assert_str(str(result.get("error", ""))).contains("Unknown key")


func test_key_press_triggers_physical_key_bound_action() -> void:
	var action: StringName = &"stagehand_test_physical_a"
	var bound: InputEventKey = InputEventKey.new()
	bound.physical_keycode = KEY_A
	InputMap.add_action(action)
	InputMap.action_add_event(action, bound)

	StagehandInputSimulator.input_key(get_tree(), {"key": "a", "hold_ms": 500})
	await get_tree().process_frame

	var pressed: bool = Input.is_action_pressed(action)
	# Wait out the 500ms hold: it exceeds after_test's settle window.
	await get_tree().create_timer(0.55, true, false, true).timeout
	InputMap.erase_action(action)
	assert_bool(pressed).is_true()


func test_key_press_sets_physical_keycode_key_label_and_unicode() -> void:
	StagehandInputSimulator.input_key(get_tree(), {"key": "a"})
	await get_tree().process_frame

	var event: InputEventKey = _recorder.first_of_type("InputEventKey")
	assert_object(event).is_not_null()
	assert_int(event.physical_keycode).is_equal(KEY_A)
	assert_int(event.key_label).is_equal(KEY_A)
	assert_int(event.unicode).is_equal(int("a".unicode_at(0)))


func test_key_press_unicode_respects_shift() -> void:
	StagehandInputSimulator.input_key(get_tree(), {"key": "a", "modifiers": ["shift"]})
	await get_tree().process_frame

	var event: InputEventKey = _recorder.first_of_type("InputEventKey")
	assert_object(event).is_not_null()
	assert_int(event.unicode).is_equal(int("A".unicode_at(0)))


func test_key_press_leaves_unicode_zero_for_non_printable_key() -> void:
	StagehandInputSimulator.input_key(get_tree(), {"key": "escape"})
	await get_tree().process_frame

	var event: InputEventKey = _recorder.first_of_type("InputEventKey")
	assert_object(event).is_not_null()
	assert_int(event.physical_keycode).is_equal(KEY_ESCAPE)
	assert_int(event.unicode).is_equal(0)


func test_key_release_sets_physical_keycode() -> void:
	StagehandInputSimulator.input_key(get_tree(), {"key": "a", "hold_ms": 0})
	await get_tree().create_timer(0.1).timeout

	var released: InputEventKey = null
	for event: InputEvent in _recorder.of_type("InputEventKey"):
		var key_event: InputEventKey = event
		# Filter by key: a prior test's hold timer can still release into this recorder.
		if not key_event.pressed and key_event.keycode == KEY_A:
			released = key_event
	assert_object(released).is_not_null()
	assert_int(released.physical_keycode).is_equal(KEY_A)
	assert_int(released.key_label).is_equal(KEY_A)


func test_key_press_types_character_into_focused_line_edit() -> void:
	var line_edit: LineEdit = auto_free(LineEdit.new())
	add_child(line_edit)
	line_edit.grab_focus()
	await get_tree().process_frame

	StagehandInputSimulator.input_key(get_tree(), {"key": "a"})
	await get_tree().process_frame
	await get_tree().process_frame

	assert_str(line_edit.text).is_equal("a")


# ── input_action ─────────────────────────────────────────────────────────

func test_action_press_pushes_action_event() -> void:
	StagehandInputSimulator.input_action(get_tree(), {"action": "ui_accept"})
	await get_tree().process_frame

	var event: InputEventAction = _recorder.first_of_type("InputEventAction")
	assert_object(event).is_not_null()
	assert_str(event.action).is_equal("ui_accept")
	assert_bool(event.pressed).is_true()


func test_action_honors_strength() -> void:
	StagehandInputSimulator.input_action(
		get_tree(), {"action": "ui_accept", "strength": 0.5}
	)
	await get_tree().process_frame

	var event: InputEventAction = _recorder.first_of_type("InputEventAction")
	assert_object(event).is_not_null()
	assert_float(event.strength).is_equal_approx(0.5, 0.001)


func test_action_reports_the_action_it_sent() -> void:
	var result: Dictionary = StagehandInputSimulator.input_action(
		get_tree(), {"action": "ui_cancel"}
	)
	assert_bool(result.get("success", false)).is_true()
	assert_str(str(result.get("action"))).is_equal("ui_cancel")


func test_action_unknown_action_returns_typed_error() -> void:
	var result: Dictionary = StagehandInputSimulator.input_action(
		get_tree(), {"action": "no_such_action"}
	)
	assert_bool(result.get("success", false)).is_false()
	assert_str(str(result.get("error_code", ""))).is_equal("invalid_params")
	assert_str(str(result.get("error", ""))).contains("no_such_action")
	await get_tree().process_frame
	# Nothing was pushed for an action the game does not define.
	assert_int(_recorder.of_type("InputEventAction").size()).is_equal(0)


func test_action_unknown_action_next_action_lists_similar_actions() -> void:
	var result: Dictionary = StagehandInputSimulator.input_action(
		get_tree(), {"action": "ui_acept"}
	)
	var details: Dictionary = result.get("details", {})
	assert_str(str(details.get("next_action", ""))).contains("ui_accept")


func test_action_missing_action_returns_error() -> void:
	var result: Dictionary = StagehandInputSimulator.input_action(get_tree(), {})
	assert_str(str(result.get("error", ""))).contains("Missing required parameter: action")


# ── input_text ───────────────────────────────────────────────────────────

func test_text_pushes_one_key_event_pair_per_character() -> void:
	# delay_ms 0 keeps the call synchronous — no per-character frame wait.
	var result: Dictionary = await StagehandInputSimulator.input_text(
		get_tree(), {"text": "hey", "delay_ms": 0}
	)
	assert_bool(result.get("success", false)).is_true()
	assert_int(result.get("chars_count", -1)).is_equal(3)

	await get_tree().process_frame
	# One press + one release per character.
	assert_int(_recorder.of_type("InputEventKey").size()).is_equal(6)


func test_text_sets_unicode_on_key_events() -> void:
	await StagehandInputSimulator.input_text(get_tree(), {"text": "Z", "delay_ms": 0})
	await get_tree().process_frame

	var event: InputEventKey = _recorder.first_of_type("InputEventKey")
	assert_object(event).is_not_null()
	assert_int(event.unicode).is_equal("Z".unicode_at(0))


func test_text_newline_submits_a_line_edit() -> void:
	var line_edit: LineEdit = auto_free(LineEdit.new())
	add_child(line_edit)
	line_edit.grab_focus()
	await get_tree().process_frame
	var submitted: Array[String] = []
	line_edit.text_submitted.connect(func(t: String) -> void: submitted.append(t))

	await StagehandInputSimulator.input_text(get_tree(), {"text": "a\n", "delay_ms": 0})
	await get_tree().process_frame
	await get_tree().process_frame

	assert_str(line_edit.text).is_equal("a")
	assert_array(submitted).is_equal(["a"])


func test_text_newline_inserts_a_line_break_in_a_text_edit() -> void:
	var text_edit: TextEdit = auto_free(TextEdit.new())
	add_child(text_edit)
	text_edit.grab_focus()
	await get_tree().process_frame

	await StagehandInputSimulator.input_text(get_tree(), {"text": "a\nb", "delay_ms": 0})
	await get_tree().process_frame
	await get_tree().process_frame

	assert_str(text_edit.text).is_equal("a\nb")


func test_mouse_release_after_the_viewport_is_freed_is_harmless() -> void:
	var viewport: SubViewport = SubViewport.new()
	add_child(viewport)
	StagehandInputSimulator._release_mouse_after(
		get_tree(), viewport, Vector2.ZERO, MOUSE_BUTTON_LEFT, 0.05
	)
	viewport.free()
	await get_tree().create_timer(0.15, true, false, true).timeout
	assert_bool(is_instance_valid(viewport)).is_false()


func test_text_echoes_what_it_typed() -> void:
	var result: Dictionary = await StagehandInputSimulator.input_text(
		get_tree(), {"text": "abc", "delay_ms": 0}
	)
	assert_str(str(result.get("typed_text"))).is_equal("abc")


func test_text_missing_text_returns_error() -> void:
	var result: Dictionary = await StagehandInputSimulator.input_text(get_tree(), {})
	assert_str(str(result.get("error", ""))).contains("Missing required parameter: text")


func test_text_unmatched_selector_returns_error() -> void:
	var result: Dictionary = await StagehandInputSimulator.input_text(
		get_tree(), {"text": "abc", "selector": "group:no_such_group_at_all"}
	)
	assert_str(str(result.get("error", ""))).contains("Node not found")


# ── Engine.time_scale = 0 ────────────────────────────────────────────────
# A pause menu commonly freezes the game with Engine.time_scale = 0. Every
# simulator timer must keep running on wall-clock time then, or a held key is
# never released and input_text never returns.

func after_test() -> void:
	Engine.time_scale = 1.0
	Input.emulate_mouse_from_touch = _saved_mouse_from_touch
	if InputMap.has_action(MOUSE_ACTION):
		Input.action_release(MOUSE_ACTION)
		InputMap.erase_action(MOUSE_ACTION)
	# The simulator schedules releases on SceneTreeTimers and queues key/action
	# events for the next frame; left pending, they land in the next test's
	# recorder. Outlast the default 100ms hold so every test starts clean.
	await get_tree().create_timer(SIMULATOR_SETTLE_SEC, true, false, true).timeout


func test_key_release_arrives_when_time_scale_is_zero() -> void:
	Engine.time_scale = 0.0
	StagehandInputSimulator.input_key(get_tree(), {"key": "a", "hold_ms": 50})
	# Wall-clock wait that the time_scale override above cannot stall.
	await get_tree().create_timer(0.3, true, false, true).timeout

	var released: bool = false
	for event: InputEvent in _recorder.of_type("InputEventKey"):
		var key_event: InputEventKey = event
		if not key_event.pressed and key_event.keycode == KEY_A:
			released = true
	assert_bool(released).is_true()


func test_text_returns_when_time_scale_is_zero() -> void:
	Engine.time_scale = 0.0
	var state: Dictionary = {"done": false}
	var run: Callable = func() -> void:
		await StagehandInputSimulator.input_text(get_tree(), {"text": "ab", "delay_ms": 20})
		state["done"] = true
	run.call()
	await get_tree().create_timer(0.5, true, false, true).timeout
	assert_bool(state["done"]).is_true()
