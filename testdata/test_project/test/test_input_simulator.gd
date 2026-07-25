# GdUnit4 assertions are fluent and return self for chaining, so every
# unchained assert_*() trips return_value_discarded=2. Scoped, deliberate
# relaxation of that one warning; all other strict warnings stay errors.
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


## A position-only click pushes the button event alone. The leading motion
## event described in input_mouse's comment is emitted from
## _gui_delivery_confirmed, which only runs for selector-driven clicks — so a
## raw coordinate click never gets one, and hover-dependent Controls will not
## see the pointer arrive. Documented here as current behavior.
func test_position_click_pushes_no_leading_motion_event() -> void:
	StagehandInputSimulator.input_mouse(get_tree(), {"position": _at(50.0, 60.0)})
	assert_int(_recorder.of_type("InputEventMouseButton").size()).is_greater_equal(1)
	assert_int(_recorder.of_type("InputEventMouseMotion").size()).is_equal(0)


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
	assert_str(str(result.get("error", ""))).contains("Missing position")
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
	assert_str(str(result.get("error", ""))).contains("Missing key")


func test_key_unknown_key_name_returns_error() -> void:
	var result: Dictionary = StagehandInputSimulator.input_key(
		get_tree(), {"key": "NotARealKeyName"}
	)
	assert_str(str(result.get("error", ""))).contains("Unknown key")


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


func test_action_missing_action_returns_error() -> void:
	var result: Dictionary = StagehandInputSimulator.input_action(get_tree(), {})
	assert_str(str(result.get("error", ""))).contains("Missing action")


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


func test_text_echoes_what_it_typed() -> void:
	var result: Dictionary = await StagehandInputSimulator.input_text(
		get_tree(), {"text": "abc", "delay_ms": 0}
	)
	assert_str(str(result.get("typed_text"))).is_equal("abc")


func test_text_missing_text_returns_error() -> void:
	var result: Dictionary = await StagehandInputSimulator.input_text(get_tree(), {})
	assert_str(str(result.get("error", ""))).contains("Missing text")


func test_text_unmatched_selector_returns_error() -> void:
	var result: Dictionary = await StagehandInputSimulator.input_text(
		get_tree(), {"text": "abc", "selector": "group:no_such_group_at_all"}
	)
	assert_str(str(result.get("error", ""))).contains("Node not found")
