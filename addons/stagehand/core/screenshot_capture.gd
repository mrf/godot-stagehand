## Captures viewport screenshots and returns base64-encoded PNG data.
class_name StagehandScreenshotCapture
extends RefCounted

const ERRORS := preload("res://addons/stagehand/core/errors.gd")
const SELECTOR_ENGINE := preload("res://addons/stagehand/core/selector_engine.gd")

const DEFAULT_READY_FRAME_TIMEOUT: int = 8
const MAX_READY_FRAME_TIMEOUT: int = 60


## Capture the viewport as a PNG base64 string.
## Optionally crops to a node's bounding rect if [param selector] is provided.
static func capture(tree: SceneTree, params: Dictionary) -> Dictionary:
	var viewport: Viewport = tree.root
	if viewport == null:
		return ERRORS.make(ERRORS.INTERNAL, "No viewport available", {
			"next_action": "Capture from a running game process; there is no viewport before the scene tree exists.",
		})

	var full_page: bool = true
	var full_page_raw: Variant = params.get("full_page", true)
	if full_page_raw is bool:
		full_page = full_page_raw
	var crop_rect: Rect2i = Rect2i()

	if not full_page and params.has("selector"):
		var selector_str: String = str(params["selector"])
		var nodes: Array[Node] = SELECTOR_ENGINE.query(tree, selector_str)
		if nodes.is_empty():
			return ERRORS.make(ERRORS.NODE_NOT_FOUND, "Selector not found: %s" % selector_str, {
				"selector": selector_str,
				"next_action": "Verify the selector matches a visible node before requesting a cropped screenshot.",
			})
		crop_rect = _get_node_rect(nodes[0])

	var image_result: Dictionary = await _capture_ready_image(tree, viewport, _get_ready_frame_timeout(params))
	if ERRORS.is_error(image_result):
		return image_result
	var img: Image = image_result["image"]

	if crop_rect != Rect2i():
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

	return {
		"data": base64,
		"mime_type": "image/png",
		"width": img.get_width(),
		"height": img.get_height(),
	}


static func _capture_ready_image(tree: SceneTree, viewport: Viewport, max_frames: int) -> Dictionary:
	for _frame_index: int in range(max_frames):
		# process_frame is guaranteed to fire in headless mode; frame_post_draw
		# can hang indefinitely when the renderer is unavailable.
		await tree.process_frame
		var texture: ViewportTexture = viewport.get_texture()
		if texture == null:
			continue
		var img: Image = texture.get_image()
		if img != null and not img.is_empty():
			return {
				"image": img,
			}

	return ERRORS.make(ERRORS.RENDERER_UNAVAILABLE, "Failed to capture viewport image after %d frame(s): image was null or zero-size" % max_frames, {
		"frames_waited": max_frames,
		"next_action": "Use a headed Godot process with a visible window for screenshot workflows; headless/no-GPU sessions may not render pixels.",
	})


static func _get_ready_frame_timeout(params: Dictionary) -> int:
	var frames: int = DEFAULT_READY_FRAME_TIMEOUT
	var raw_frames: Variant = params.get("capture_timeout_frames", DEFAULT_READY_FRAME_TIMEOUT)
	if raw_frames is int:
		frames = raw_frames
	if frames < 1:
		frames = 1
	if frames > MAX_READY_FRAME_TIMEOUT:
		frames = MAX_READY_FRAME_TIMEOUT
	return frames


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



## Get the screen-space bounding rect for a node.
static func _get_node_rect(node: Node) -> Rect2i:
	if node is Control:
		var ctrl: Control = node
		return Rect2i(ctrl.global_position, ctrl.size)
	if node is Node2D:
		var n2d: Node2D = node
		# Best-effort: try to find a collision or sprite shape.
		if node.has_node("CollisionShape2D"):
			var shape_node: CollisionShape2D = node.get_node("CollisionShape2D")
			var shape: Shape2D = shape_node.shape
			if shape is RectangleShape2D:
				var rs: RectangleShape2D = shape
				var half: Vector2 = rs.size / 2.0
				var global_pos: Vector2 = shape_node.global_position
				return Rect2i(global_pos - half, rs.size)
		if node.has_node("Sprite2D"):
			var sprite: Sprite2D = node.get_node("Sprite2D")
			if sprite.texture != null:
				var size: Vector2 = sprite.texture.get_size() * sprite.global_scale
				return Rect2i(
					Vector2i(sprite.global_position - size / 2.0),
					Vector2i(size)
				)
		# Fallback: a single-pixel point at the global position.
		return Rect2i(Vector2i(n2d.global_position), Vector2i(1, 1))
	return Rect2i()
