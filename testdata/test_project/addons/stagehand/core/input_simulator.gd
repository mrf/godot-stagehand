## Synthesizes InputEvent objects to simulate user input.
class_name StagehandInputSimulator
extends RefCounted

const ERRORS := preload("res://addons/stagehand/core/errors.gd")
const SELECTOR_ENGINE := preload("res://addons/stagehand/core/selector_engine.gd")

const BUTTON_MAP: Dictionary = {
	"left": MOUSE_BUTTON_LEFT,
	"right": MOUSE_BUTTON_RIGHT,
	"middle": MOUSE_BUTTON_MIDDLE,
}


## The [Viewport] a synthesized pointer event should be delivered to, and the
## canvas-space (i.e. [Control]/[Node2D] local coordinate space, not window
## pixels) position within that viewport. See [method _resolve_click_target].
class ClickTarget extends RefCounted:
	## The viewport the event is pushed into. For a node inside an embedded
	## subwindow this is the outermost non-embedded [Window], not the node's own
	## viewport — the embedder owns hit-testing for its subwindows.
	var viewport: Viewport
	## [member viewport]-space position of the click.
	var position: Vector2
	## The viewport that actually hit-tests the event and whose GUI state
	## records the result — the node's own viewport. Equal to [member viewport]
	## except when the node lives inside an embedded subwindow.
	var hit_viewport: Viewport


## Canonical failure for a node that exists but cannot take part in the
## requested interaction. Shared by every selector-driven input path so the same
## condition always reports the same kind and the same context.
static func _not_supported(
	selector: String, node: Node, operation: String, next_action: String
) -> Dictionary:
	return ERRORS.make(
		ERRORS.NOT_SUPPORTED,
		"Node type does not support %s: %s is a %s" % [operation, node.get_path(), node.get_class()],
		{
			"selector": selector,
			"node_path": str(node.get_path()),
			"node_class": node.get_class(),
			"operation": operation,
			"next_action": next_action,
		}
	)


## Simulate a mouse click at [param position] (canvas coordinates).
## [param button] is "left", "right", or "middle".
## [param double_click] triggers a double-click event.
static func input_mouse(tree: SceneTree, params: Dictionary) -> Dictionary:
	var has_selector: bool = params.has("selector")
	var has_position: bool = params.has("position")

	if not has_selector and not has_position:
		return ERRORS.make(ERRORS.INVALID_PARAMS, "Missing selector or position", {
			"next_action": "Supply either a selector to click, or an explicit {x, y} position.",
		})

	var target: ClickTarget
	var matched_count: int = 0
	var clicked_node: Node = null
	if has_selector:
		var nodes: Array[Node] = SELECTOR_ENGINE.query(tree, str(params["selector"]))
		if nodes.is_empty():
			return ERRORS.node_not_found(str(params["selector"]))
		matched_count = nodes.size()
		# When a selector resolves to several nodes (e.g. a descriptive Label and
		# the actual Button both containing the word), prefer the interactive one
		# instead of silently clicking the first match in tree order.
		var ranked: Array[Node] = SELECTOR_ENGINE.rank_for_interaction(nodes)
		clicked_node = ranked[0]
		if clicked_node is CanvasItem:
			var ci: CanvasItem = clicked_node
			target = _resolve_click_target(ci)
		else:
			return _not_supported(
				str(params["selector"]), clicked_node, "clicking",
				"Target a CanvasItem (Control or Node2D); non-visual nodes cannot receive pointer events."
			)
	else:
		var p: Dictionary = params["position"]
		target = ClickTarget.new()
		target.viewport = tree.root
		target.hit_viewport = tree.root
		target.position = Vector2(_v_float(p.get("x", 0)), _v_float(p.get("y", 0)))

	# Checked before anything is pushed. Refusing after the fact is not good
	# enough: the swallowed button event also costs the modal its window focus,
	# which breaks every subsequent key event (see _blocking_modal).
	var blocker: Window = _blocking_modal(target.viewport, target.position)
	if blocker != null:
		return _modal_blocked(blocker, target.position, "this click")

	var btn_str: String = params.get("button", "left")
	var btn: int = _v_int(BUTTON_MAP.get(btn_str, MOUSE_BUTTON_LEFT))
	var double_click: bool = _v_bool(params.get("double_click", false))

	# A leading motion event mirrors how a real pointer always moves before it
	# clicks, so it is always sent regardless of whether this is a
	# selector-driven or raw position click. For selector-driven clicks it also
	# lets us confirm the click will actually land on the intended Control
	# before we report success — see _gui_delivery_confirmed for why that
	# confirmation can't be made for raw position-only clicks.
	_push_mouse_motion(target.viewport, target.position)
	if has_selector and not _gui_delivery_confirmed(target, clicked_node):
		return ERRORS.make(
			ERRORS.NOT_SUPPORTED,
			"Click target did not receive the event: %s is not the topmost Control at (%.1f, %.1f)" % [
				clicked_node.get_path(), target.position.x, target.position.y,
			],
			{
				"selector": str(params["selector"]),
				"node_path": str(clicked_node.get_path()),
				"clicked_at": {"x": target.position.x, "y": target.position.y},
				"next_action": "Something is covering the target — dismiss the overlay, or click the topmost Control directly.",
			}
		)

	_push_mouse_button(target.viewport, target.position, btn, true, double_click)
	var hold_ms: int = _v_int(params.get("hold_ms", 100))
	_release_mouse_after(tree, target.viewport, target.position, btn, hold_ms / 1000.0)

	var result: Dictionary = {
		"success": true,
		"clicked_at": {"x": target.position.x, "y": target.position.y},
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
		return ERRORS.missing_param("action")
	var strength: float = _v_float(params.get("strength", 1.0))
	var hold_ms: int = _v_int(params.get("hold_ms", 100))

	_press_action(action, strength)
	_release_action_after(tree, action, hold_ms / 1000.0)

	return {"success": true, "action": action}


## Simulate pressing and releasing a keyboard key.
static func input_key(tree: SceneTree, params: Dictionary) -> Dictionary:
	var key_str: String = params.get("key", "")
	if key_str.is_empty():
		return ERRORS.missing_param("key")

	var keycode: int = OS.find_keycode_from_string(key_str)
	if keycode == KEY_NONE:
		return ERRORS.make(ERRORS.INVALID_PARAMS, "Unknown key: %s" % key_str, {
			"key": key_str,
			"next_action": "Use a Godot key name such as \"escape\", \"enter\", \"a\", or \"f1\".",
		})

	var unfocused: Window = _unfocused_modal(tree.root)
	if unfocused != null:
		return ERRORS.make(
			ERRORS.NOT_SUPPORTED,
			"Key input cannot reach modal dialog %s: it is visible but does not have window focus" % [
				unfocused.get_path(),
			],
			{
				"key": key_str,
				"blocking_window": str(unfocused.get_path()),
				"next_action": "Click a Control inside the dialog to focus it, then send the key.",
			}
		)

	var modifiers: Array = params.get("modifiers", [])
	var mod_mask: int = _parse_modifiers(modifiers)
	var hold_ms: int = _v_int(params.get("hold_ms", 100))

	_press_key(keycode, mod_mask)
	_release_key_after(tree, keycode, mod_mask, hold_ms / 1000.0)

	return {"success": true, "key": key_str}


## Delivers [param event] directly to [param viewport]'s input/GUI dispatch
## using local (canvas-space) coordinates, instead of routing it through
## [method Input.parse_input_event] and the [DisplayServer] window pixel space.
##
## Under --headless, Godot never creates a real OS window, so the root
## [Window]'s reported [member Window.size] is a degenerate stub (observed
## 64x64 against a real Godot 4.6.2 binary) regardless of the project's
## configured resolution. [method Viewport.push_input] with
## in_local_coords=true skips [method Input.parse_input_event]'s window-pixel
## coordinate remap, but Godot's GUI dispatch still hit-tests every pointer
## event against the target [Window]'s own size — confirmed with
## instrumentation: [method Viewport.gui_get_hovered_control] stays null for a
## click computed at a real [Control]'s on-screen position until
## [method _ensure_headless_window_sized] corrects that stub, at which point
## it correctly reports the target [Control] (godot-stagehand-nry). Canvas
## coordinates are otherwise correct in both the windowed
## content-scale-stretch case (godot-stagehand-phase3-vrj.19) and the
## headless case, making the separate window/canvas stretch-transform mapping
## this addon previously needed unnecessary.
static func _push(viewport: Viewport, event: InputEvent) -> void:
	if viewport == null:
		Input.parse_input_event(event)
		return
	_ensure_headless_window_sized(viewport)
	viewport.push_input(event, true)


## Corrects the degenerate root-[Window] size stub described on [method _push]
## so GUI input dispatch has real bounds to hit-test synthesized pointer
## events against. Gated on [method DisplayServer.get_name] == "headless"
## (confirmed the reported name under --headless) so a real windowed
## session — where [member Window.size] is legitimate and may deliberately
## differ from the project's configured resolution (user-resized window,
## content-scale stretch, etc.) — is never touched, preserving the
## godot-stagehand-phase3-vrj.19 stretch-mode fix. Only [Window] targets are
## corrected; a [SubViewport]'s size is developer-controlled already and
## unaffected by this headless quirk.
static func _ensure_headless_window_sized(viewport: Viewport) -> void:
	if not (viewport is Window):
		return
	if DisplayServer.get_name() != "headless":
		return
	var window: Window = viewport
	var wanted: Vector2i = Vector2i(
		_v_int(ProjectSettings.get_setting("display/window/size/viewport_width", 1152)),
		_v_int(ProjectSettings.get_setting("display/window/size/viewport_height", 648))
	)
	if wanted.x <= 0 or wanted.y <= 0:
		return
	if window.size != wanted:
		window.size = wanted


static func _push_mouse_motion(viewport: Viewport, pos: Vector2) -> void:
	var ev: InputEventMouseMotion = InputEventMouseMotion.new()
	ev.position = pos
	ev.global_position = pos
	ev.relative = Vector2.ZERO
	_push(viewport, ev)


static func _push_mouse_button(viewport: Viewport, pos: Vector2, btn: int, pressed: bool, double_click: bool) -> void:
	var ev: InputEventMouseButton = InputEventMouseButton.new()
	ev.position = pos
	ev.global_position = pos
	ev.button_index = btn as MouseButton
	ev.pressed = pressed
	ev.double_click = double_click
	_push(viewport, ev)


## Best-effort confirmation that a click at [param target]'s position will
## actually be observed by [param expected_node]. Called after the leading
## motion event (see input_mouse) has already been pushed, and inspects
## [method Viewport.gui_get_hovered_control] — the same lookup Godot's own GUI
## system uses to decide who a pointer event belongs to.
##
## This can only validate [Control] targets reached via a selector: a raw
## position click has no "expected" node (clicking empty space to dismiss a
## popup is legitimate), and a [Node2D] click has no equivalent engine-level
## "who received this" signal to query, so [param expected_node] is null in
## both of those cases and this always reports delivered. That gap is the
## documented limit of what's detectable here — see the AC discussion in
## godot-stagehand-nry.
static func _gui_delivery_confirmed(target: ClickTarget, expected_node: Node) -> bool:
	if expected_node == null or target.hit_viewport == null:
		return true
	# The lookup goes to the viewport that hit-tests the node, which is the
	# subwindow itself when the target lives inside one — the embedder the event
	# was pushed into records no hover for its subwindows' contents.
	var hovered: Control = target.hit_viewport.gui_get_hovered_control()
	if hovered == null:
		return false
	if hovered == expected_node:
		return true
	# A child inside the target's own rect (e.g. a Label/TextureRect painted
	# over a Button) still counts as the target receiving the interaction.
	return expected_node.is_ancestor_of(hovered)


static func _release_mouse_after(tree: SceneTree, viewport: Viewport, pos: Vector2, btn: int, delay_sec: float) -> void:
	var timer: SceneTreeTimer = tree.create_timer(delay_sec)
	var _err: int = timer.timeout.connect(func() -> void:
		_push_mouse_button(viewport, pos, btn, false, false)
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
		return ERRORS.missing_param("text")

	var delay_ms: int = _v_int(params.get("delay_ms", 50))
	# Optional selector to click first to gain focus
	if params.has("selector"):
		var nodes: Array[Node] = SELECTOR_ENGINE.query(tree, str(params["selector"]))
		if nodes.is_empty():
			return ERRORS.node_not_found(str(params["selector"]))
		# Prefer an interactive control so we focus the input, not a nearby label.
		var node: Node = SELECTOR_ENGINE.rank_for_interaction(nodes)[0]
		if not (node is CanvasItem):
			return _not_supported(
				str(params["selector"]), node, "focusing",
				"Target a focusable Control such as LineEdit or TextEdit."
			)
		var ci: CanvasItem = node
		var target: ClickTarget = _resolve_click_target(ci)
		var blocker: Window = _blocking_modal(target.viewport, target.position)
		if blocker != null:
			return _modal_blocked(blocker, target.position, "the focus click")

		# Click the node to give it focus before typing.
		_push_mouse_button(target.viewport, target.position, MOUSE_BUTTON_LEFT, true, false)
		_push_mouse_button(target.viewport, target.position, MOUSE_BUTTON_LEFT, false, false)

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
		return ERRORS.missing_param("position")

	var viewport: Viewport = tree.root
	var p: Dictionary = params["position"]
	var pos: Vector2 = Vector2(_v_float(p.get("x", 0.0)), _v_float(p.get("y", 0.0)))
	var index: int = _v_int(params.get("index", 0))
	var action: String = params.get("action", "tap")
	var duration_ms: int = _v_int(params.get("duration_ms", 100))

	var blocker: Window = _blocking_modal(viewport, pos)
	if blocker != null:
		return _modal_blocked(blocker, pos, "this touch")

	match action:
		"tap":
			_touch_press(viewport, pos, index)
			if params.has("drag_to"):
				var dt: Dictionary = params["drag_to"]
				var drag_pos: Vector2 = Vector2(_v_float(dt.get("x", 0.0)), _v_float(dt.get("y", 0.0)))
				_touch_drag(viewport, pos, drag_pos, index)
				_touch_release_after(tree, viewport, drag_pos, index, duration_ms / 1000.0)
				return {
					"success": true,
					"position": {"x": pos.x, "y": pos.y},
					"drag_to": {"x": drag_pos.x, "y": drag_pos.y},
					"index": index,
				}
			_touch_release_after(tree, viewport, pos, index, duration_ms / 1000.0)
			return {"success": true, "position": {"x": pos.x, "y": pos.y}, "index": index}
		"begin":
			_touch_press(viewport, pos, index)
			return {"success": true, "position": {"x": pos.x, "y": pos.y}, "index": index, "action": "begin"}
		"move":
			if not params.has("drag_to"):
				return ERRORS.make(ERRORS.INVALID_PARAMS, "drag_to is required for action 'move'", {
					"action": action,
					"next_action": "Supply drag_to as {x, y}, or use action \"tap\"/\"begin\"/\"end\".",
				})
			var dt: Dictionary = params["drag_to"]
			var drag_pos: Vector2 = Vector2(_v_float(dt.get("x", 0.0)), _v_float(dt.get("y", 0.0)))
			_touch_drag(viewport, pos, drag_pos, index)
			return {
				"success": true,
				"from": {"x": pos.x, "y": pos.y},
				"to": {"x": drag_pos.x, "y": drag_pos.y},
				"index": index,
				"action": "move",
			}
		"end":
			_touch_release(viewport, pos, index)
			return {"success": true, "position": {"x": pos.x, "y": pos.y}, "index": index, "action": "end"}
		_:
			return ERRORS.make(ERRORS.INVALID_PARAMS, "Unknown action: %s" % action, {
				"action": action,
				"known_actions": ["tap", "begin", "move", "end"],
			})


static func _touch_press(viewport: Viewport, pos: Vector2, index: int) -> void:
	var ev: InputEventScreenTouch = InputEventScreenTouch.new()
	ev.position = pos
	ev.index = index
	ev.pressed = true
	_push(viewport, ev)


static func _touch_release(viewport: Viewport, pos: Vector2, index: int) -> void:
	var ev: InputEventScreenTouch = InputEventScreenTouch.new()
	ev.position = pos
	ev.index = index
	ev.pressed = false
	_push(viewport, ev)


static func _touch_release_after(tree: SceneTree, viewport: Viewport, pos: Vector2, index: int, delay_sec: float) -> void:
	var timer: SceneTreeTimer = tree.create_timer(delay_sec)
	var _err: int = timer.timeout.connect(func() -> void:
		_touch_release(viewport, pos, index)
	)


static func _touch_drag(viewport: Viewport, from: Vector2, to: Vector2, index: int) -> void:
	var ev: InputEventScreenDrag = InputEventScreenDrag.new()
	ev.position = to
	ev.index = index
	ev.relative = to - from
	ev.velocity = Vector2.ZERO
	_push(viewport, ev)


## Moves mouse cursor to specified position without clicking
static func input_mouse_move(tree: SceneTree, params: Dictionary) -> Dictionary:
	var target: ClickTarget
	if params.has("selector"):
		var nodes: Array[Node] = SELECTOR_ENGINE.query(tree, str(params["selector"]))
		if nodes.is_empty():
			return ERRORS.node_not_found(str(params["selector"]))
		# Prefer an interactive control when the selector is ambiguous.
		var node: Node = SELECTOR_ENGINE.rank_for_interaction(nodes)[0]
		if not (node is CanvasItem):
			return _not_supported(
				str(params["selector"]), node, "mouse positioning",
				"Target a CanvasItem (Control or Node2D) that has an on-screen rect."
			)
		var ci: CanvasItem = node
		target = _resolve_click_target(ci)
	elif params.has("coordinates"):
		var coords: Dictionary = params["coordinates"]
		target = ClickTarget.new()
		target.viewport = tree.root
		target.hit_viewport = tree.root
		target.position = Vector2(_v_float(coords.get("x", 0)), _v_float(coords.get("y", 0)))
	else:
		return ERRORS.make(
			ERRORS.INVALID_PARAMS, "Either selector or coordinates is required", {
				"next_action": "Supply a selector to move onto, or explicit {x, y} coordinates.",
			}
		)

	_push_mouse_motion(target.viewport, target.position)

	return {
		"success": true,
		"moved_to": {"x": target.position.x, "y": target.position.y},
		"mode": "by_selector" if params.has("selector") else "absolute",
	}


## Resolves the [Viewport] a selector-matched node's synthesized pointer
## event should be delivered to, and the node's click target in that
## viewport's own canvas (local) coordinate space.
## [method CanvasItem.get_global_transform_with_canvas] resolves the node's
## on-canvas position, honoring CanvasLayer and Camera2D transforms; for a
## [Control] the target is its center, for a [Node2D] its origin.
##
## When the node lives inside an embedded subwindow — any modal dialog:
## [AcceptDialog], [ConfirmationDialog], [FileDialog] — that canvas point is in
## the subwindow's own space, and pushing it into the subwindow's own
## [method Viewport.push_input] does nothing at all: an embedded [Window] is
## hit-tested by its embedder, not by itself. Confirmed against Godot 4.6.2:
## pushing a motion event at the in-subwindow point into the subwindow leaves
## [method Viewport.gui_get_hovered_control] null, while the same event
## translated into the embedder and pushed there reports the target [Control]
## and makes a click actually fire [signal BaseButton.pressed]
## (godot-stagehand-growth-distribution-87s.21). So walk out of every embedded
## [Window] in the chain, mapping the point through each one's own
## canvas-to-pixel transform and offsetting by its position — which, for input
## routing, the engine treats as being in the embedder's canvas space, the same
## space [method Viewport.push_input] with in_local_coords=true expects.
##
## A node in the main window is untouched by the loop (the root [Window] is not
## embedded), so it keeps clicking at its plain canvas point, preserving the
## content-scale-stretch behaviour established by
## godot-stagehand-phase3-vrj.19. A [SubViewport] is likewise left alone: it is
## not a [Window], and it hit-tests its own input.
static func _resolve_click_target(node: CanvasItem) -> ClickTarget:
	var local_target: Vector2 = Vector2.ZERO
	if node is Control:
		var ctrl: Control = node
		local_target = ctrl.size / 2.0
	var point: Vector2 = node.get_global_transform_with_canvas() * local_target
	var viewport: Viewport = node.get_viewport()

	var target: ClickTarget = ClickTarget.new()
	target.hit_viewport = viewport
	while viewport is Window:
		var window: Window = viewport
		if not window.is_embedded():
			break
		var embedder: Viewport = _embedder_of(window)
		if embedder == null:
			break
		point = window.get_final_transform() * point + Vector2(window.position)
		viewport = embedder

	target.position = point
	target.viewport = viewport
	return target


## The [Viewport] that hosts [param window] as an embedded subwindow, or null
## when it has no parent. [Node.get_viewport] on a [Viewport] returns that
## viewport itself, so the lookup has to start from the parent.
static func _embedder_of(window: Window) -> Viewport:
	var parent: Node = window.get_parent()
	if parent == null:
		return null
	return parent.get_viewport()


## The visible exclusive (modal) embedded subwindow of [param viewport] that
## would swallow a pointer event at [param point], or null when nothing blocks
## it. [param point] is in [param viewport]'s canvas space.
##
## A modal subwindow makes the engine discard pointer events landing outside
## its own rect. Confirmed against Godot 4.6.2 with a node recording raw
## [method Node._input] on the main window, clicking/touching at the same point
## with an [AcceptDialog] hidden and then popped up
## (godot-stagehand-growth-distribution-87s.21):
##
## [codeblock]
## modal HIDDEN -> root _input saw touches=2 buttons=2
## modal SHOWN, touch outside -> touches=0 focus kept=true
## modal SHOWN, click outside -> buttons=0 focus kept=false
## [/codeblock]
##
## Both event kinds are swallowed outright, and a mouse button additionally
## drops the dialog's window focus — after which every later key event is lost
## too (see [method _unfocused_modal]). Reporting that click as a success is
## what let a caller believe it had dismissed a first-run splash dialog when it
## had in fact only broken its own keyboard path.
static func _blocking_modal(viewport: Viewport, point: Vector2) -> Window:
	if viewport == null:
		return null
	var subwindows: Array[Window] = viewport.get_embedded_subwindows()
	# Topmost first: the engine hit-tests subwindows front to back, and only the
	# frontmost modal can block.
	for i: int in range(subwindows.size() - 1, -1, -1):
		var window: Window = subwindows[i]
		if not window.visible:
			continue
		if not window.exclusive:
			# A non-exclusive popup dismisses itself on an outside click rather
			# than eating it, which is legitimate behaviour to drive.
			continue
		if Rect2(Vector2(window.position), Vector2(window.size)).has_point(point):
			return null
		return window
	return null


## A visible exclusive (modal) embedded subwindow of [param viewport] that does
## not hold window focus, or null when none does.
##
## Key events synthesized through [method Input.parse_input_event] are routed by
## the engine to the focused window, so a modal that has lost focus receives
## none of them. Confirmed against Godot 4.6.2: a freshly popped [AcceptDialog]
## has focus and closes on a synthesized Escape, but once a pointer event has
## landed outside it — see [method _blocking_modal] — [method Window.has_focus]
## goes false and the identical Escape leaves the dialog open with no error
## anywhere (godot-stagehand-growth-distribution-87s.21). Callers hitting this
## used to get `{"success": true}` and an unchanged screen.
static func _unfocused_modal(viewport: Viewport) -> Window:
	if viewport == null:
		return null
	for window: Window in viewport.get_embedded_subwindows():
		if not window.visible or not window.exclusive:
			continue
		if not window.has_focus():
			return window
	return null


## Canonical refusal for a pointer event a modal subwindow would swallow.
static func _modal_blocked(window: Window, point: Vector2, operation: String) -> Dictionary:
	return ERRORS.make(
		ERRORS.NOT_SUPPORTED,
		"Modal dialog %s blocks %s at (%.1f, %.1f): the event would be discarded" % [
			window.get_path(), operation, point.x, point.y,
		],
		{
			"blocking_window": str(window.get_path()),
			"blocking_window_rect": {
				"x": float(window.position.x),
				"y": float(window.position.y),
				"width": float(window.size.x),
				"height": float(window.size.y),
			},
			"attempted_at": {"x": point.x, "y": point.y},
			"next_action": "Interact with the modal dialog first — target a Control inside it, or dismiss it.",
		}
	)


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
