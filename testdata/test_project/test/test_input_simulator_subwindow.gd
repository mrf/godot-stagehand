# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for synthesized input against embedded [Window] subwindows — the
## modal-dialog case (AcceptDialog, ConfirmationDialog, FileDialog, …) that
## godot-stagehand-growth-distribution-87s.21 found Stagehand could not drive.
##
## Two distinct engine behaviours are pinned here, both confirmed against a
## real Godot 4.6.2 binary (traces in the 87s.21 commit message):
##
## 1. A [Control] inside an embedded subwindow lives in that subwindow's own
##    coordinate space. Pushing at those coordinates into the subwindow's own
##    [method Viewport.push_input] is inert — the embedder owns hit-testing, so
##    the event has to be translated into the embedder's space and pushed
##    there.
## 2. A visible exclusive (modal) embedded subwindow swallows pointer events
##    outside its own rect AND silently drops the embedder window's focus,
##    which then loses every subsequent key event. Both used to be reported as
##    success.
##
## The suite deliberately drives a non-1.0 [member Viewport.content_scale_factor]
## so the content-vs-window coordinate mapping (phase3-vrj.19's defect class)
## is exercised at the same time.

const INNER_GROUP: StringName = &"subwindow_inner_button"
const OUTER_GROUP: StringName = &"subwindow_outer_button"

## Matches the headless root-window size StagehandInputSimulator normalises to,
## so the dialog's popup_centered() geometry cannot shift mid-test.
const ROOT_SIZE: Vector2i = Vector2i(1152, 648)
const CONTENT_SCALE: float = 1.5

var _saved_size: Vector2i
var _saved_scale: float
var _dialog: AcceptDialog
var _inner: Button
var _outer: Button
var _inner_presses: int = 0
var _outer_presses: int = 0


func before_test() -> void:
	var root: Window = get_tree().root
	_saved_size = root.size
	_saved_scale = root.content_scale_factor
	root.size = ROOT_SIZE
	root.content_scale_factor = CONTENT_SCALE

	_inner_presses = 0
	_outer_presses = 0

	# A Control in the main window, well clear of where the dialog pops up. It
	# is the witness for "the modal swallowed this click".
	_outer = auto_free(Button.new())
	_outer.name = "OutsideDialogButton"
	_outer.position = Vector2(10.0, 10.0)
	_outer.size = Vector2(120.0, 40.0)
	_outer.add_to_group(OUTER_GROUP)
	_outer.pressed.connect(func() -> void: _outer_presses += 1)
	add_child(_outer)

	_dialog = auto_free(AcceptDialog.new())
	_dialog.name = "SubwindowSplashDialog"
	_inner = auto_free(Button.new())
	_inner.name = "InsideDialogButton"
	_inner.text = "New Project"
	_inner.custom_minimum_size = Vector2(290.0, 27.0)
	_inner.add_to_group(INNER_GROUP)
	_inner.pressed.connect(func() -> void: _inner_presses += 1)
	_dialog.add_child(_inner)
	add_child(_dialog)
	await get_tree().process_frame

	_dialog.popup_centered(Vector2i(360, 220))
	await get_tree().process_frame
	await get_tree().process_frame


func after_test() -> void:
	var root: Window = get_tree().root
	root.content_scale_factor = _saved_scale
	root.size = _saved_size


## The coordinate the fixed simulator is expected to click: the button's centre
## in its own subwindow, translated into the embedder's input space.
func _expected_embedder_point() -> Vector2:
	var in_dialog: Vector2 = _inner.get_global_transform_with_canvas() * (_inner.size / 2.0)
	return _dialog.get_final_transform() * in_dialog + Vector2(_dialog.position)


func _at(x: float, y: float) -> Dictionary:
	return {"x": x, "y": y}


# ── (a) coordinates: reaching a Control inside an embedded subwindow ─────

## hold_ms 0 keeps the synthesized release on the very next frame — a Button
## emits `pressed` on button-up, so the default 100ms hold would outlive the
## test's frame waits.
func test_selector_click_inside_a_modal_subwindow_presses_the_button() -> void:
	var result: Dictionary = StagehandInputSimulator.input_mouse(
		get_tree(), {"selector": "group:%s" % INNER_GROUP, "hold_ms": 0}
	)
	assert_str(str(result.get("error", ""))).is_empty()
	assert_bool(result.get("success", false)).is_true()
	await get_tree().process_frame
	await get_tree().process_frame
	assert_int(_inner_presses).is_equal(1)


func test_selector_click_in_a_subwindow_reports_embedder_space_coordinates() -> void:
	var result: Dictionary = StagehandInputSimulator.input_mouse(
		get_tree(), {"selector": "group:%s" % INNER_GROUP}
	)
	var expected: Vector2 = _expected_embedder_point()
	var clicked: Dictionary = result.get("clicked_at", {})
	assert_float(clicked.get("x", -1.0)).is_equal_approx(expected.x, 0.001)
	assert_float(clicked.get("y", -1.0)).is_equal_approx(expected.y, 0.001)
	# The subwindow is offset inside the embedder, so the embedder-space point
	# must differ from the raw in-subwindow point the addon used to click.
	var in_dialog: Vector2 = _inner.get_global_transform_with_canvas() * (_inner.size / 2.0)
	assert_vector(expected).is_not_equal(in_dialog)


func test_mouse_move_onto_a_control_inside_a_subwindow_hovers_it() -> void:
	var result: Dictionary = StagehandInputSimulator.input_mouse_move(
		get_tree(), {"selector": "group:%s" % INNER_GROUP}
	)
	assert_bool(result.get("success", false)).is_true()
	assert_object(_dialog.gui_get_hovered_control()).is_same(_inner)


# ── (b) modal blocking: a swallowed event must not report success ────────

func test_position_click_outside_a_modal_subwindow_returns_a_typed_failure() -> void:
	var result: Dictionary = StagehandInputSimulator.input_mouse(
		get_tree(), {"position": _at(60.0, 30.0)}
	)
	assert_bool(result.get("success", false)).is_false()
	assert_str(str(result.get("error_code", ""))).is_equal("not_supported")
	assert_str(str(result.get("error", ""))).contains(str(_dialog.get_path()))
	await get_tree().process_frame
	assert_int(_outer_presses).is_equal(0)


func test_selector_click_outside_a_modal_subwindow_returns_a_typed_failure() -> void:
	var result: Dictionary = StagehandInputSimulator.input_mouse(
		get_tree(), {"selector": "group:%s" % OUTER_GROUP}
	)
	assert_bool(result.get("success", false)).is_false()
	assert_str(str(result.get("error_code", ""))).is_equal("not_supported")
	await get_tree().process_frame
	assert_int(_outer_presses).is_equal(0)


## The engine drops the modal's focus when a pointer event lands outside it,
## which silently breaks every later key event. A refused click must not
## have pushed anything, so focus survives.
func test_a_refused_click_leaves_the_modal_focused() -> void:
	assert_bool(_dialog.has_focus()).is_true()
	StagehandInputSimulator.input_mouse(get_tree(), {"position": _at(60.0, 30.0)})
	await get_tree().process_frame
	assert_bool(_dialog.has_focus()).is_true()


func test_position_click_inside_the_modal_rect_is_allowed() -> void:
	var centre: Vector2 = Vector2(_dialog.position) + Vector2(_dialog.size) / 2.0
	var result: Dictionary = StagehandInputSimulator.input_mouse(
		get_tree(), {"position": _at(centre.x, centre.y)}
	)
	assert_str(str(result.get("error", ""))).is_empty()
	assert_bool(result.get("success", false)).is_true()


func test_touch_outside_a_modal_subwindow_returns_a_typed_failure() -> void:
	var result: Dictionary = StagehandInputSimulator.input_touch(
		get_tree(), {"position": _at(60.0, 30.0)}
	)
	assert_bool(result.get("success", false)).is_false()
	assert_str(str(result.get("error_code", ""))).is_equal("not_supported")


# ── (b) key input: an unfocused modal loses every key ────────────────────

func test_key_input_while_the_modal_is_unfocused_returns_a_typed_failure() -> void:
	# Reproduce the focus loss the way the engine does it, bypassing the
	# simulator so the guard under test is not the thing setting up the state.
	for pressed: bool in [true, false]:
		var ev: InputEventMouseButton = InputEventMouseButton.new()
		ev.position = Vector2(60.0, 30.0)
		ev.global_position = ev.position
		ev.button_index = MOUSE_BUTTON_LEFT
		ev.pressed = pressed
		get_tree().root.push_input(ev, true)
	await get_tree().process_frame
	assert_bool(_dialog.has_focus()).is_false()

	var result: Dictionary = StagehandInputSimulator.input_key(get_tree(), {"key": "Escape"})
	assert_bool(result.get("success", false)).is_false()
	assert_str(str(result.get("error_code", ""))).is_equal("not_supported")
	assert_str(str(result.get("error", ""))).contains(str(_dialog.get_path()))


func test_key_input_reaches_a_focused_modal_subwindow() -> void:
	assert_bool(_dialog.has_focus()).is_true()
	var result: Dictionary = StagehandInputSimulator.input_key(get_tree(), {"key": "Escape"})
	assert_bool(result.get("success", false)).is_true()
	await get_tree().process_frame
	await get_tree().process_frame
	assert_bool(_dialog.visible).is_false()


# ── regression: the main-window path must keep phase3-vrj.19's behaviour ──

func test_main_window_control_still_clicks_in_canvas_coordinates_under_content_scale() -> void:
	# With the modal dismissed, a plain main-window Control must still be
	# clicked at its canvas point — NOT at its content-scaled window pixel —
	# which is what phase3-vrj.19 established.
	_dialog.hide()
	await get_tree().process_frame

	var result: Dictionary = StagehandInputSimulator.input_mouse(
		get_tree(), {"selector": "group:%s" % OUTER_GROUP, "hold_ms": 0}
	)
	assert_bool(result.get("success", false)).is_true()
	var expected: Vector2 = _outer.get_global_transform_with_canvas() * (_outer.size / 2.0)
	var clicked: Dictionary = result.get("clicked_at", {})
	assert_float(clicked.get("x", -1.0)).is_equal_approx(expected.x, 0.001)
	assert_float(clicked.get("y", -1.0)).is_equal_approx(expected.y, 0.001)
	await get_tree().process_frame
	await get_tree().process_frame
	assert_int(_outer_presses).is_equal(1)
