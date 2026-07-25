extends SceneTree
## Checks Stagehand against a REAL, visible, non-embedded (operating-system)
## [Window] — the case test/test_input_simulator_os_subwindow.gd cannot cover
## (godot-stagehand-inpw).
##
## Lives outside test/ and runs as its own throwaway process, deliberately.
## Popping a non-embedded [Window] permanently breaks
## [method Input.parse_input_event] delivery for the remainder of the process
## (traced in that suite's docstring), so this cannot share the GdUnit runner
## with the key/action/text suites. Run it under a real display server to get a
## genuine OS-window answer rather than a headless approximation:
##
## [codeblock]
## godot --path testdata/test_project -s os_subwindow_probe.gd            # windowed
## godot --headless --path testdata/test_project -s os_subwindow_probe.gd # headless
## [/codeblock]
##
## Emits one [code]OSWIN <key>=<value>[/code] line per observation and exits
## non-zero if any expectation is unmet, so os_subwindow_gate_test.go can assert
## on it. Nothing here is a Stagehand behaviour choice — the delivery lines are
## raw engine facts, and if a future Godot starts delivering synthesized pointer
## events to OS windows this fails and the refusal should be revisited.

const SIM := preload("res://addons/stagehand/core/input_simulator.gd")

const GROUP: StringName = &"os_probe_button"
const REQUESTED_DIALOG_SIZE: Vector2i = Vector2i(360, 220)

var _presses: int = 0
var _failures: PackedStringArray = PackedStringArray()


func _say(key: String, value: Variant) -> void:
	print("OSWIN %s=%s" % [key, value])


func _expect(key: String, actual: Variant, wanted: Variant) -> void:
	_say(key, actual)
	if actual != wanted:
		var _appended: bool = _failures.append(
			"%s: expected %s, got %s" % [key, wanted, actual]
		)


func _initialize() -> void:
	_say("display_server", DisplayServer.get_name())

	root.size = Vector2i(1152, 648)
	root.gui_embed_subwindows = false

	var dialog: AcceptDialog = AcceptDialog.new()
	dialog.name = "OsProbeDialog"
	var button: Button = Button.new()
	button.name = "OsProbeButton"
	button.text = "New Project"
	button.custom_minimum_size = Vector2(290.0, 27.0)
	button.add_to_group(GROUP)
	var _connected: int = button.pressed.connect(func() -> void: _presses += 1)
	dialog.add_child(button)
	root.add_child(dialog)
	await process_frame

	dialog.popup_centered(REQUESTED_DIALOG_SIZE)
	await process_frame
	await process_frame

	# The premise. A display server that embeds anyway would make every line
	# below a statement about the embedded path wearing an OS-window label.
	_expect("embedded", dialog.is_embedded(), false)
	# Recorded, not asserted: headless clamps popup_centered() to the dialog's
	# minimum size, so the value differs per display server. What has to hold is
	# that Stagehand leaves it alone — see window_size_unchanged below.
	var popped_size: Vector2i = dialog.size
	_say("window_size", popped_size)

	await _probe_raw_engine_delivery(dialog, button)
	_probe_stagehand_refusal(dialog)
	# _ensure_headless_window_sized used to rewrite this to the project
	# resolution, silently resizing the application's own dialog.
	_expect("window_size_unchanged", dialog.size == popped_size, true)

	if _failures.is_empty():
		print("OSWIN result=ok")
		quit(0)
		return
	for failure: String in _failures:
		printerr("OSWIN FAILURE %s" % failure)
	print("OSWIN result=failed")
	quit(1)


## Raw engine behaviour, with Stagehand entirely out of the picture: push the
## same motion/press/release sequence a real pointer would produce straight into
## the OS window and see whether anything happens.
func _probe_raw_engine_delivery(dialog: AcceptDialog, button: Button) -> void:
	var point: Vector2 = button.get_global_transform_with_canvas() * (button.size / 2.0)
	_say("push_point", point)

	var motion: InputEventMouseMotion = InputEventMouseMotion.new()
	motion.position = point
	motion.global_position = point
	dialog.push_input(motion, true)
	await process_frame
	_expect("hovered_control", str(dialog.gui_get_hovered_control()), "<Object#null>")

	for pressed: bool in [true, false]:
		var click: InputEventMouseButton = InputEventMouseButton.new()
		click.position = point
		click.global_position = point
		click.button_index = MOUSE_BUTTON_LEFT
		click.pressed = pressed
		dialog.push_input(click, true)
		await process_frame
	await process_frame
	_expect("button_presses", _presses, 0)


## Stagehand's own answer for the same target: a typed refusal naming the window
## and the project setting, not a success and not an overlay accusation.
func _probe_stagehand_refusal(dialog: AcceptDialog) -> void:
	var result: Dictionary = SIM.input_mouse(
		root.get_tree(), {"selector": "group:%s" % GROUP, "hold_ms": 0}
	)
	_expect("click_success", result.get("success", false), false)
	_expect("click_error_code", str(result.get("error_code", "")), "not_supported")
	_expect("click_names_window", str(result.get("error", "")).contains(str(dialog.get_path())), true)
	_expect("click_blames_overlay", str(result.get("error", "")).contains("topmost Control"), false)

	var details: Dictionary = result.get("details", {})
	_expect(
		"click_next_action_names_setting",
		str(details.get("next_action", "")).contains("embed_subwindows"),
		true
	)

	var moved: Dictionary = SIM.input_mouse_move(root.get_tree(), {"selector": "group:%s" % GROUP})
	_expect("move_success", moved.get("success", false), false)
	_expect("move_error_code", str(moved.get("error_code", "")), "not_supported")
