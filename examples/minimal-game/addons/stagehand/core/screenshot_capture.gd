## Captures viewport screenshots and returns base64-encoded PNG data.
class_name StagehandScreenshotCapture
extends RefCounted

const ERRORS := preload("res://addons/stagehand/core/errors.gd")
const PARAMS := preload("res://addons/stagehand/core/params.gd")
const SELECTOR_ENGINE := preload("res://addons/stagehand/core/selector_engine.gd")

const DEFAULT_READY_FRAME_TIMEOUT: int = 8
const MAX_READY_FRAME_TIMEOUT: int = 60
## Draws that must land after the request before a read counts as fresh. The
## second is a whole process-to-draw cycle that began after the request, so
## anything an earlier call left queued (deferred calls, transform
## notifications) has reached the renderer.
const FRESH_DRAWS_REQUIRED: int = 2

## The digest of the last image handed back and the camera pose it was requested
## at. A read that repeats the digest from a different pose has not caught up.
static var _previous_capture: Dictionary = {}


## Everything the capture reads off the engine, gathered in one object so a
## headless test can script a renderer that --headless cannot provide.
class RenderView:
	extends RefCounted

	func read_image(viewport: Viewport) -> Image:
		var texture: ViewportTexture = viewport.get_texture()
		if texture == null:
			return null
		return texture.get_image()

	func frames_drawn() -> int:
		return Engine.get_frames_drawn()

	## Whether [param window]'s own render target is being updated. The frame
	## counter cannot answer this: a minimized game was measured advancing it
	## while its image stayed byte-identical.
	func can_draw(window: Window) -> bool:
		var id: int = window.get_window_id()
		return (
			DisplayServer.window_can_draw(id)
			and DisplayServer.window_get_mode(id) != DisplayServer.WINDOW_MODE_MINIMIZED
		)

	func window_mode_of(window: Window) -> DisplayServer.WindowMode:
		return DisplayServer.window_get_mode(window.get_window_id())

	## low_processor_usage_mode skips the draw when nothing changed, and then the
	## held image is the current one.
	func is_idle_and_current() -> bool:
		return OS.low_processor_usage_mode and not RenderingServer.has_changed()

	## An exact encoding of the active camera's pose, empty when there is none.
	## Bytes rather than the value, because the 3D and 2D poses are different
	## types and comparing those raises at runtime.
	func camera_pose(viewport: Viewport) -> PackedByteArray:
		var camera_3d: Camera3D = viewport.get_camera_3d()
		if camera_3d != null:
			return var_to_bytes(camera_3d.global_transform)
		var camera_2d: Camera2D = viewport.get_camera_2d()
		if camera_2d != null:
			return var_to_bytes(camera_2d.get_screen_center_position())
		return PackedByteArray()


## Capture the viewport as a PNG base64 string.
## Optionally crops to a node's bounding rect if [param selector] is provided.
static func capture(tree: SceneTree, params: Dictionary, render: RenderView = null) -> Dictionary:
	if render == null:
		render = RenderView.new()
	var viewport: Viewport = tree.root
	if viewport == null:
		return ERRORS.make(ERRORS.INTERNAL, "No viewport available", {
			"next_action": "Capture from a running game process; there is no viewport before the scene tree exists.",
		})

	var invalid: Dictionary = PARAMS.check(params, {
		"full_page": TYPE_BOOL, "selector": TYPE_STRING, "capture_timeout_frames": TYPE_INT,
	})
	if not invalid.is_empty():
		return invalid
	var full_page: bool = PARAMS.get_bool(params, "full_page", true)
	var has_crop: bool = false
	var crop_rect: Rect2i = Rect2i()

	if not full_page and params.has("selector"):
		var selector_str: String = str(params["selector"])
		var nodes: Array[Node] = SELECTOR_ENGINE.query(tree, selector_str)
		if nodes.is_empty():
			return ERRORS.make(ERRORS.NODE_NOT_FOUND, "Selector not found: %s" % selector_str, {
				"selector": selector_str,
				"next_action": "Verify the selector matches a visible node before requesting a cropped screenshot.",
			})
		var rect_result: Dictionary = _get_node_rect(nodes[0], viewport.get_final_transform())
		if ERRORS.is_error(rect_result):
			return rect_result
		crop_rect = rect_result["rect"]
		has_crop = true

	var image_result: Dictionary = await _capture_ready_image(tree, viewport, _get_ready_frame_timeout(params), render)
	if ERRORS.is_error(image_result):
		return image_result
	var img: Image = image_result["image"]

	if has_crop:
		var crop_result: Dictionary = _validate_crop_rect(crop_rect, Vector2i(img.get_width(), img.get_height()))
		if ERRORS.is_error(crop_result):
			return crop_result
		var clamped: Rect2i = crop_result["rect"]
		img = img.get_region(clamped)
		if img == null or img.is_empty():
			return ERRORS.make(ERRORS.INVALID_PARAMS, "Cropped screenshot image is empty", {
				"crop": _rect_details(clamped),
				"next_action": "Check that the selected node is visible and has a non-zero on-screen rect.",
			})

	var buffer: PackedByteArray = img.save_png_to_buffer()
	if buffer.is_empty():
		return ERRORS.make(ERRORS.INTERNAL, "PNG encode produced zero bytes for a %dx%d image" % [img.get_width(), img.get_height()], {
			"width": img.get_width(),
			"height": img.get_height(),
			"next_action": "Run Godot with a visible rendered window; headless or GPU-less sessions may not provide screenshot pixels.",
		})
	var base64: String = Marshalls.raw_to_base64(buffer)
	if base64.is_empty():
		return ERRORS.make(ERRORS.INTERNAL, "base64 encode produced an empty string from %d PNG bytes" % buffer.size(), {
			"png_bytes": buffer.size(),
			"next_action": "Retry the capture and check Godot logs for encoder errors.",
		})

	var response: Dictionary = {
		"data": base64,
		"mime_type": "image/png",
		"width": img.get_width(),
		"height": img.get_height(),
		"frame": image_result["frame"],
		"fresh_draws": image_result["fresh_draws"],
	}
	if image_result.has("unchanged_after_camera_move"):
		response["unchanged_after_camera_move"] = true
	return response


## Wait for a frame drawn after this request and read it. Three outcomes, so a
## held image never passes for a fresh one: the fresh image, a repeat of the
## previous capture marked as such, or an error naming why nothing was drawn.
static func _capture_ready_image(tree: SceneTree, viewport: Viewport, max_frames: int, render: RenderView) -> Dictionary:
	var drawn_at_request: int = render.frames_drawn()
	var pose: PackedByteArray = render.camera_pose(viewport)
	for _frame_index: int in range(max_frames):
		# process_frame is guaranteed to fire in headless mode; frame_post_draw
		# can hang indefinitely when the renderer is unavailable.
		await tree.process_frame
		if not render.can_draw(tree.root):
			continue
		if render.frames_drawn() - drawn_at_request < FRESH_DRAWS_REQUIRED:
			continue
		var img: Image = render.read_image(viewport)
		if img == null or img.is_empty():
			continue
		var digest: String = _digest(img)
		if _repeats_previous_capture(digest, pose):
			continue
		return _accept_image(img, digest, pose, render, drawn_at_request)
	return _read_with_budget_spent(tree, viewport, max_frames, render, drawn_at_request, pose)


## The wait ran out, so this read is the last word. A full-screen menu over a
## moving 3D camera really does repeat its pixels, which is why a repeat comes
## back marked rather than refused.
static func _read_with_budget_spent(tree: SceneTree, viewport: Viewport, max_frames: int, render: RenderView, drawn_at_request: int, pose: PackedByteArray) -> Dictionary:
	var img: Image = render.read_image(viewport)
	if img == null or img.is_empty():
		return ERRORS.make(ERRORS.RENDERER_UNAVAILABLE, "Failed to capture viewport image after %d frame(s): image was null or zero-size" % max_frames, {
			"frames_waited": max_frames,
			"next_action": "Use a headed Godot process with a visible window for screenshot workflows; headless/no-GPU sessions may not render pixels.",
		})

	var draws: int = render.frames_drawn() - drawn_at_request
	if render.can_draw(tree.root) and (draws > 0 or render.is_idle_and_current()):
		return _accept_image(img, _digest(img), pose, render, drawn_at_request)

	return ERRORS.make(ERRORS.STALE_FRAME, "Viewport was not redrawn during the %d frame(s) this capture waited; its image predates the request" % max_frames, {
		"frames_waited": max_frames,
		"frames_drawn": draws,
		"window_mode": render.window_mode_of(tree.root),
		"next_action": "Un-minimize the game window (DisplayServer.window_set_mode) and check that the render loop is enabled, then capture again.",
	})


static func _accept_image(img: Image, digest: String, pose: PackedByteArray, render: RenderView, drawn_at_request: int) -> Dictionary:
	var frame: int = render.frames_drawn()
	var accepted: Dictionary = {
		"image": img,
		"frame": frame,
		"fresh_draws": frame - drawn_at_request,
	}
	# A repeat keeps the pose its image really came from, so retrying from the
	# new pose stays flagged for as long as the image does not change.
	if _repeats_previous_capture(digest, pose):
		accepted["unchanged_after_camera_move"] = true
	else:
		_previous_capture = {"digest": digest, "pose": pose}
	return accepted


static func _repeats_previous_capture(digest: String, pose: PackedByteArray) -> bool:
	if _previous_capture.is_empty():
		return false
	return _previous_capture["digest"] == digest and _previous_capture["pose"] != pose


static func _digest(img: Image) -> String:
	var context: HashingContext = HashingContext.new()
	var _started: Error = context.start(HashingContext.HASH_MD5)
	var _updated: Error = context.update(img.get_data())
	return context.finish().hex_encode()


static func _get_ready_frame_timeout(params: Dictionary) -> int:
	return PARAMS.get_int(params, "capture_timeout_frames", DEFAULT_READY_FRAME_TIMEOUT, 1, MAX_READY_FRAME_TIMEOUT)


static func _validate_crop_rect(crop_rect: Rect2i, image_size: Vector2i) -> Dictionary:
	var left: int = clampi(crop_rect.position.x, 0, image_size.x)
	var top: int = clampi(crop_rect.position.y, 0, image_size.y)
	var right: int = clampi(crop_rect.position.x + crop_rect.size.x, 0, image_size.x)
	var bottom: int = clampi(crop_rect.position.y + crop_rect.size.y, 0, image_size.y)

	if right <= left or bottom <= top:
		return ERRORS.make(ERRORS.INVALID_PARAMS, "Crop rect is outside the captured viewport", {
			"crop": _rect_details(crop_rect),
			"viewport_width": image_size.x,
			"viewport_height": image_size.y,
			"next_action": "Make sure the selected node is visible inside the viewport before requesting a cropped screenshot.",
		})

	return {
		"rect": Rect2i(Vector2i(left, top), Vector2i(right - left, bottom - top)),
	}


static func _rect_details(rect: Rect2i) -> Dictionary:
	return {
		"x": rect.position.x,
		"y": rect.position.y,
		"width": rect.size.x,
		"height": rect.size.y,
	}


## Bounding box, in captured-image pixels, of [param local_rect] (in the node's
## own space) after [param canvas_xform] — the node's
## [method CanvasItem.get_global_transform_with_canvas], which carries
## CanvasLayer and Camera2D — and then [param stretch_xform] — the root
## viewport's [method Viewport.get_final_transform], which carries window
## content-scale stretch. Same composition [code]input_simulator.gd[/code]
## uses to aim clicks. Rotation widens the box; it is not masked.
static func _screen_rect(local_rect: Rect2, canvas_xform: Transform2D, stretch_xform: Transform2D) -> Rect2i:
	var xform: Transform2D = stretch_xform * canvas_xform
	var corners: Array[Vector2] = [
		xform * local_rect.position,
		xform * Vector2(local_rect.end.x, local_rect.position.y),
		xform * Vector2(local_rect.position.x, local_rect.end.y),
		xform * local_rect.end,
	]
	var lo: Vector2 = corners[0]
	var hi: Vector2 = corners[0]
	for corner: Vector2 in corners:
		lo = lo.min(corner)
		hi = hi.max(corner)
	return Rect2i(Vector2i(lo.round()), Vector2i((hi - lo).round()))



## Compute the screen-space bounding rect for a node.
##
## Returns [code]{"rect": Rect2i}[/code] on success. Returns an
## [constant ERRORS.INVALID_PARAMS] envelope when the node's type has no
## defined on-screen rect (anything but [Control]/[Node2D]) or when the
## computed rect is degenerate (zero width or height) — both are explicit
## failures, never a silent full-page fallback.
static func _get_node_rect(node: Node, stretch_xform: Transform2D = Transform2D.IDENTITY) -> Dictionary:
	var rect: Rect2i
	if node is Control:
		var ctrl: Control = node
		rect = _screen_rect(Rect2(Vector2.ZERO, ctrl.size), ctrl.get_global_transform_with_canvas(), stretch_xform)
	elif node is Node2D:
		var n2d: Node2D = node
		var resolved: bool = false
		# Best-effort: try to find a collision or sprite shape.
		if node.has_node("CollisionShape2D"):
			var shape_node: CollisionShape2D = node.get_node("CollisionShape2D")
			var shape: Shape2D = shape_node.shape
			if shape is RectangleShape2D:
				var rs: RectangleShape2D = shape
				rect = _screen_rect(Rect2(-rs.size / 2.0, rs.size), shape_node.get_global_transform_with_canvas(), stretch_xform)
				resolved = true
		if not resolved and node.has_node("Sprite2D"):
			var sprite: Sprite2D = node.get_node("Sprite2D")
			if sprite.texture != null:
				var tex_size: Vector2 = sprite.texture.get_size()
				rect = _screen_rect(Rect2(-tex_size / 2.0, tex_size), sprite.get_global_transform_with_canvas(), stretch_xform)
				resolved = true
		if not resolved:
			# Fallback: a single-pixel point at the node's on-screen origin.
			var origin: Vector2 = stretch_xform * n2d.get_global_transform_with_canvas().origin
			rect = Rect2i(Vector2i(origin), Vector2i(1, 1))
	else:
		return ERRORS.make(ERRORS.INVALID_PARAMS, "Cannot compute an on-screen rect for node type %s; cropped screenshots support Control and Node2D nodes only" % node.get_class(), {
			"node_class": node.get_class(),
			"node_path": str(node.get_path()),
			"next_action": "Select a Control or Node2D node, or request a full-page screenshot instead.",
		})

	if rect.size.x <= 0 or rect.size.y <= 0:
		return ERRORS.make(ERRORS.INVALID_PARAMS, "Node %s resolved to a zero-sized rect; nothing to crop" % str(node.get_path()), {
			"node_path": str(node.get_path()),
			"rect": _rect_details(rect),
			"next_action": "Select a node with a non-zero on-screen size, or request a full-page screenshot instead.",
		})

	return {"rect": rect}
