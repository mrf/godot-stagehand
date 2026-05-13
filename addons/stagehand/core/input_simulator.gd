## Synthesizes InputEvent objects to simulate user input.
class_name StagehandInputSimulator
extends RefCounted

const SelectorEngine := preload("res://addons/stagehand/core/selector_engine.gd")

const BUTTON_MAP: Dictionary = {
	"left": MOUSE_BUTTON_LEFT,
	"right": MOUSE_BUTTON_RIGHT,
	"middle": MOUSE_BUTTON_MIDDLE,
}


## Simulate a mouse click at [param position] (screen coordinates).
## [param button] is "left", "right", or "middle".
## [param double_click] triggers a double-click event.
static func input_mouse(tree: SceneTree, params: Dictionary) -> Dictionary:
	var has_selector := params.has("selector")
	var has_position := params.has("position")

	if not has_selector and not has_position:
		return {"error": "Missing selector or position"}

	var pos: Vector2
	if has_selector:
		var nodes: Array[Node] = SelectorEngine.query(tree, str(params["selector"]))
		if nodes.is_empty():
			return {"error": "Node not found for selector"}
		var node: Node = nodes[0]
		if node is Control:
			var ctrl: Control = node
			pos = ctrl.global_position + ctrl.size / 2.0
		elif node is Node2D:
			var n2d: Node2D = node
			pos = n2d.global_position
		else:
			return {"error": "Node type does not support clicking"}
	else:
		var p: Dictionary = params["position"]
		pos = Vector2(float(p.get("x", 0)), float(p.get("y", 0)))

	var btn_str: String = params.get("button", "left")
	var btn: int = BUTTON_MAP.get(btn_str, MOUSE_BUTTON_LEFT)
	var double_click: bool = params.get("double_click", false)

	_press_mouse(tree, pos, btn, double_click)
	var hold_ms: int = params.get("hold_ms", 100)
	_release_mouse_after(tree, pos, btn, hold_ms / 1000.0)

	return {
		"success": true,
		"clicked_at": {"x": pos.x, "y": pos.y},
		"button": btn_str,
	}


## Simulate pressing and releasing an input action.
static func input_action(tree: SceneTree, params: Dictionary) -> Dictionary:
	var action: String = params.get("action", "")
	if action.is_empty():
		return {"error": "Missing action"}
	var strength: float = float(params.get("strength", 1.0))
	var hold_ms: int = int(params.get("hold_ms", 100))

	_press_action(action, strength)
	_release_action_after(tree, action, hold_ms / 1000.0)

	return {"success": true, "action": action}


## Simulate pressing and releasing a keyboard key.
static func input_key(tree: SceneTree, params: Dictionary) -> Dictionary:
	var key_str: String = params.get("key", "")
	if key_str.is_empty():
		return {"error": "Missing key"}

	var keycode: int = OS.find_keycode_from_string(key_str)
	if keycode == KEY_NONE:
		return {"error": "Unknown key: %s" % key_str}

	var modifiers: Array = params.get("modifiers", [])
	var mod_mask: int = _parse_modifiers(modifiers)
	var hold_ms: int = int(params.get("hold_ms", 100))

	_press_key(keycode, mod_mask)
	_release_key_after(tree, keycode, mod_mask, hold_ms / 1000.0)

	return {"success": true, "key": key_str}


static func _press_mouse(tree: SceneTree, pos: Vector2, btn: int, double_click: bool) -> void:
	var ev := InputEventMouseButton.new()
	ev.position = pos
	ev.global_position = pos
	ev.button_index = btn
	ev.pressed = true
	ev.double_click = double_click
	Input.parse_input_event(ev)


static func _release_mouse_after(tree: SceneTree, pos: Vector2, btn: int, delay_sec: float) -> void:
	var timer: SceneTreeTimer = tree.create_timer(delay_sec)
	timer.timeout.connect(func() -> void:
		var ev := InputEventMouseButton.new()
		ev.position = pos
		ev.global_position = pos
		ev.button_index = btn
		ev.pressed = false
		Input.parse_input_event(ev)
	)


static func _press_action(action: String, strength: float) -> void:
	var ev := InputEventAction.new()
	ev.action = action
	ev.strength = strength
	ev.pressed = true
	Input.parse_input_event(ev)


static func _release_action_after(tree: SceneTree, action: String, delay_sec: float) -> void:
	var timer: SceneTreeTimer = tree.create_timer(delay_sec)
	timer.timeout.connect(func() -> void:
		var ev := InputEventAction.new()
		ev.action = action
		ev.strength = 0.0
		ev.pressed = false
		Input.parse_input_event(ev)
	)


static func _press_key(keycode: int, mod_mask: int) -> void:
	var ev := InputEventKey.new()
	ev.keycode = keycode
	ev.pressed = true
	ev.shift_pressed = bool(mod_mask & KEY_MASK_SHIFT)
	ev.ctrl_pressed = bool(mod_mask & KEY_MASK_CTRL)
	ev.alt_pressed = bool(mod_mask & KEY_MASK_ALT)
	ev.meta_pressed = bool(mod_mask & KEY_MASK_META)
	Input.parse_input_event(ev)


static func _release_key_after(tree: SceneTree, keycode: int, mod_mask: int, delay_sec: float) -> void:
	var timer: SceneTreeTimer = tree.create_timer(delay_sec)
	timer.timeout.connect(func() -> void:
		var ev := InputEventKey.new()
		ev.keycode = keycode
		ev.pressed = false
		ev.shift_pressed = bool(mod_mask & KEY_MASK_SHIFT)
		ev.ctrl_pressed = bool(mod_mask & KEY_MASK_CTRL)
		ev.alt_pressed = bool(mod_mask & KEY_MASK_ALT)
		ev.meta_pressed = bool(mod_mask & KEY_MASK_META)
		Input.parse_input_event(ev)
	)


static func _parse_modifiers(modifiers: Array) -> int:
	var mask: int = 0
	for m: Variant in modifiers:
		match str(m).to_lower():
			"shift": mask |= KEY_MASK_SHIFT
			"ctrl", "control": mask |= KEY_MASK_CTRL
			"alt": mask |= KEY_MASK_ALT
			"meta", "cmd", "command", "win", "super": mask |= KEY_MASK_META
	return mask
