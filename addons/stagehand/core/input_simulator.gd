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
	var has_selector: bool = params.has("selector")
	var has_position: bool = params.has("position")

	if not has_selector and not has_position:
		return {"error": "Missing selector or position"}

	var pos: Vector2
	var matched_count: int = 0
	var clicked_node: Node = null
	if has_selector:
		var nodes: Array[Node] = SelectorEngine.query(tree, str(params["selector"]))
		if nodes.is_empty():
			return {"error": "Node not found for selector"}
		matched_count = nodes.size()
		# When a selector resolves to several nodes (e.g. a descriptive Label and
		# the actual Button both containing the word), prefer the interactive one
		# instead of silently clicking the first match in tree order.
		var ranked: Array[Node] = SelectorEngine.rank_for_interaction(nodes)
		clicked_node = ranked[0]
		if clicked_node is CanvasItem:
			# Selector rects live in canvas (content-scale) space; translate the
			# chosen target into window space so clicks land correctly under stretch.
			var ci: CanvasItem = clicked_node
			pos = _node_window_position(ci)
		else:
			return {"error": "Node type does not support clicking"}
	else:
		var p: Dictionary = params["position"]
		pos = Vector2(_v_float(p.get("x", 0)), _v_float(p.get("y", 0)))

	var btn_str: String = params.get("button", "left")
	var btn: int = _v_int(BUTTON_MAP.get(btn_str, MOUSE_BUTTON_LEFT))
	var double_click: bool = _v_bool(params.get("double_click", false))

	_press_mouse(tree, pos, btn, double_click)
	var hold_ms: int = _v_int(params.get("hold_ms", 100))
	_release_mouse_after(tree, pos, btn, hold_ms / 1000.0)

	var result: Dictionary = {
		"success": true,
		"clicked_at": {"x": pos.x, "y": pos.y},
		"button": btn_str,
	}
	if has_selector:
		result["matched"] = matched_count
		result["clicked_node"] = str(clicked_node.get_path())
		# Surface ambiguity so callers know the selector was not unique.
		if matched_count > 1:
			result["ambiguous"] = true
	return result


## Simulate pressing and releasing an input action.
static func input_action(tree: SceneTree, params: Dictionary) -> Dictionary:
	var action: String = params.get("action", "")
	if action.is_empty():
		return {"error": "Missing action"}
	var strength: float = _v_float(params.get("strength", 1.0))
	var hold_ms: int = _v_int(params.get("hold_ms", 100))

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
	var hold_ms: int = _v_int(params.get("hold_ms", 100))

	_press_key(keycode, mod_mask)
	_release_key_after(tree, keycode, mod_mask, hold_ms / 1000.0)

	return {"success": true, "key": key_str}


static func _press_mouse(_tree: SceneTree, pos: Vector2, btn: int, double_click: bool) -> void:
	var ev: InputEventMouseButton = InputEventMouseButton.new()
	ev.position = pos
	ev.global_position = pos
	ev.button_index = btn as MouseButton
	ev.pressed = true
	ev.double_click = double_click
	Input.parse_input_event(ev)


static func _release_mouse_after(tree: SceneTree, pos: Vector2, btn: int, delay_sec: float) -> void:
	var timer: SceneTreeTimer = tree.create_timer(delay_sec)
	var _err: int = timer.timeout.connect(func() -> void:
		var ev: InputEventMouseButton = InputEventMouseButton.new()
		ev.position = pos
		ev.global_position = pos
		ev.button_index = btn as MouseButton
		ev.pressed = false
		Input.parse_input_event(ev)
	)


static func _press_action(action: String, strength: float) -> void:
	var ev: InputEventAction = InputEventAction.new()
	ev.action = action
	ev.strength = strength
	ev.pressed = true
	Input.parse_input_event(ev)


static func _release_action_after(tree: SceneTree, action: String, delay_sec: float) -> void:
	var timer: SceneTreeTimer = tree.create_timer(delay_sec)
	var _err: int = timer.timeout.connect(func() -> void:
		var ev: InputEventAction = InputEventAction.new()
		ev.action = action
		ev.strength = 0.0
		ev.pressed = false
		Input.parse_input_event(ev)
	)


static func _press_key(keycode: int, mod_mask: int) -> void:
	var ev: InputEventKey = InputEventKey.new()
	ev.keycode = keycode as Key
	ev.pressed = true
	ev.shift_pressed = (mod_mask & KEY_MASK_SHIFT) != 0
	ev.ctrl_pressed = (mod_mask & KEY_MASK_CTRL) != 0
	ev.alt_pressed = (mod_mask & KEY_MASK_ALT) != 0
	ev.meta_pressed = (mod_mask & KEY_MASK_META) != 0
	Input.parse_input_event(ev)


static func _release_key_after(tree: SceneTree, keycode: int, mod_mask: int, delay_sec: float) -> void:
	var timer: SceneTreeTimer = tree.create_timer(delay_sec)
	var _err: int = timer.timeout.connect(func() -> void:
		var ev: InputEventKey = InputEventKey.new()
		ev.keycode = keycode as Key
		ev.pressed = false
		ev.shift_pressed = (mod_mask & KEY_MASK_SHIFT) != 0
		ev.ctrl_pressed = (mod_mask & KEY_MASK_CTRL) != 0
		ev.alt_pressed = (mod_mask & KEY_MASK_ALT) != 0
		ev.meta_pressed = (mod_mask & KEY_MASK_META) != 0
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


## Types text into the currently focused control
static func input_text(tree: SceneTree, params: Dictionary) -> Dictionary:
	var text: String = params.get("text", "")
	if text.is_empty():
		return {"error": "Missing text"}

	var delay_ms: int = _v_int(params.get("delay_ms", 50))
	# Optional selector to click first to gain focus
	if params.has("selector"):
		var nodes: Array[Node] = SelectorEngine.query(tree, str(params["selector"]))
		if nodes.is_empty():
			return {"error": "Node not found for selector"}
		# Prefer an interactive control so we focus the input, not a nearby label.
		var node: Node = SelectorEngine.rank_for_interaction(nodes)[0]
		# Click the node to give it focus before typing
		var pos: Vector2
		if node is CanvasItem:
			var ci: CanvasItem = node
			pos = _node_window_position(ci)
		else:
			return {"error": "Node type does not support focusing"}

		# Emit mouse click to focus the control
		var ev_click: InputEventMouseButton = InputEventMouseButton.new()
		ev_click.position = pos
		ev_click.global_position = pos
		ev_click.button_index = MOUSE_BUTTON_LEFT
		ev_click.pressed = true
		Input.parse_input_event(ev_click)

		var release_ev: InputEventMouseButton = InputEventMouseButton.new()
		release_ev.position = pos
		release_ev.global_position = pos
		release_ev.button_index = MOUSE_BUTTON_LEFT
		release_ev.pressed = false
		Input.parse_input_event(release_ev)

	var chars: PackedStringArray = text.split("", false)
	var total_delay: float = 0.0
	for ch: String in chars:
		var event: InputEventKey = InputEventKey.new()
		event.unicode = ch.unicode_at(0)
		event.keycode = KEY_NONE as Key
		event.pressed = true
		Input.parse_input_event(event)

		var release_event: InputEventKey = InputEventKey.new()
		release_event.unicode = ch.unicode_at(0)
		release_event.keycode = KEY_NONE as Key
		release_event.pressed = false
		Input.parse_input_event(release_event)

		if delay_ms > 0:
			await tree.create_timer(delay_ms / 1000.0).timeout
			total_delay += delay_ms / 1000.0

	return {"success": true, "typed_text": text, "chars_count": chars.size(), "total_delay": total_delay}


## Simulate a touch screen event.
static func input_touch(tree: SceneTree, params: Dictionary) -> Dictionary:
	if not params.has("position"):
		return {"error": "Missing position"}

	var p: Dictionary = params["position"]
	var pos: Vector2 = Vector2(_v_float(p.get("x", 0.0)), _v_float(p.get("y", 0.0)))
	var index: int = _v_int(params.get("index", 0))
	var action: String = params.get("action", "tap")
	var duration_ms: int = _v_int(params.get("duration_ms", 100))

	match action:
		"tap":
			_touch_press(pos, index)
			if params.has("drag_to"):
				var dt: Dictionary = params["drag_to"]
				var drag_pos: Vector2 = Vector2(_v_float(dt.get("x", 0.0)), _v_float(dt.get("y", 0.0)))
				_touch_drag(pos, drag_pos, index)
				_touch_release_after(tree, drag_pos, index, duration_ms / 1000.0)
				return {
					"success": true,
					"position": {"x": pos.x, "y": pos.y},
					"drag_to": {"x": drag_pos.x, "y": drag_pos.y},
					"index": index,
				}
			_touch_release_after(tree, pos, index, duration_ms / 1000.0)
			return {"success": true, "position": {"x": pos.x, "y": pos.y}, "index": index}
		"begin":
			_touch_press(pos, index)
			return {"success": true, "position": {"x": pos.x, "y": pos.y}, "index": index, "action": "begin"}
		"move":
			if not params.has("drag_to"):
				return {"error": "drag_to is required for action 'move'"}
			var dt: Dictionary = params["drag_to"]
			var drag_pos: Vector2 = Vector2(_v_float(dt.get("x", 0.0)), _v_float(dt.get("y", 0.0)))
			_touch_drag(pos, drag_pos, index)
			return {
				"success": true,
				"from": {"x": pos.x, "y": pos.y},
				"to": {"x": drag_pos.x, "y": drag_pos.y},
				"index": index,
				"action": "move",
			}
		"end":
			_touch_release(pos, index)
			return {"success": true, "position": {"x": pos.x, "y": pos.y}, "index": index, "action": "end"}
		_:
			return {"error": "Unknown action: %s" % action}


static func _touch_press(pos: Vector2, index: int) -> void:
	var ev: InputEventScreenTouch = InputEventScreenTouch.new()
	ev.position = pos
	ev.index = index
	ev.pressed = true
	Input.parse_input_event(ev)


static func _touch_release(pos: Vector2, index: int) -> void:
	var ev: InputEventScreenTouch = InputEventScreenTouch.new()
	ev.position = pos
	ev.index = index
	ev.pressed = false
	Input.parse_input_event(ev)


static func _touch_release_after(tree: SceneTree, pos: Vector2, index: int, delay_sec: float) -> void:
	var timer: SceneTreeTimer = tree.create_timer(delay_sec)
	var _err: int = timer.timeout.connect(func() -> void:
		_touch_release(pos, index)
	)


static func _touch_drag(from: Vector2, to: Vector2, index: int) -> void:
	var ev: InputEventScreenDrag = InputEventScreenDrag.new()
	ev.position = to
	ev.index = index
	ev.relative = to - from
	ev.velocity = Vector2.ZERO
	Input.parse_input_event(ev)


## Moves mouse cursor to specified position without clicking
static func input_mouse_move(tree: SceneTree, params: Dictionary) -> Dictionary:
	var pos: Vector2
	if params.has("selector"):
		var nodes: Array[Node] = SelectorEngine.query(tree, str(params["selector"]))
		if nodes.is_empty():
			return {"error": "Node not found for selector"}
		# Prefer an interactive control when the selector is ambiguous.
		var node: Node = SelectorEngine.rank_for_interaction(nodes)[0]

		if node is CanvasItem:
			var ci: CanvasItem = node
			pos = _node_window_position(ci)
		else:
			return {"error": "Node type does not support mouse positioning"}
	elif params.has("coordinates"):
		var coords: Dictionary = params["coordinates"]
		pos = Vector2(_v_float(coords.get("x", 0)), _v_float(coords.get("y", 0)))
	else:
		return {"error": "Either selector or coordinates is required"}

	var ev_motion: InputEventMouseMotion = InputEventMouseMotion.new()
	ev_motion.position = pos
	ev_motion.global_position = pos
	ev_motion.relative = Vector2.ZERO
	Input.parse_input_event(ev_motion)

	return {
		"success": true,
		"moved_to": {"x": pos.x, "y": pos.y},
		"mode": "by_selector" if params.has("selector") else "absolute",
	}


## Converts a selector-matched node's click target into window/display coordinates.
##
## Selector-derived node rects live in the viewport's canvas (content-scale) space,
## but [method Input.parse_input_event] delivers events in window/display pixels.
## When the project uses content-scale stretch (window size differs from the
## content scale size), those spaces diverge, so a position computed in canvas
## space lands off-target by the stretch ratio (see godot-stagehand-phase3-vrj.19).
## We map canvas -> window via the viewport's stretch transform.
## [method CanvasItem.get_global_transform_with_canvas] resolves the node's
## on-canvas position, honoring CanvasLayer and Camera2D transforms; for a
## [Control] the target is its center, for a [Node2D] its origin.
static func _node_window_position(node: CanvasItem) -> Vector2:
	var local_target: Vector2 = Vector2.ZERO
	if node is Control:
		var ctrl: Control = node
		local_target = ctrl.size / 2.0
	var canvas_point: Vector2 = node.get_global_transform_with_canvas() * local_target
	var viewport: Viewport = node.get_viewport()
	if viewport == null:
		return canvas_point
	return viewport.get_stretch_transform() * canvas_point


static func _v_int(v: Variant) -> int:
	if v is int:
		return v
	if v is float:
		var f: float = v
		return int(f)
	return 0


static func _v_float(v: Variant) -> float:
	if v is float:
		return v
	if v is int:
		var i: int = v
		return float(i)
	return 0.0


static func _v_bool(v: Variant) -> bool:
	if v is bool:
		return v
	return false
