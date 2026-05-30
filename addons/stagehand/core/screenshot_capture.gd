## Captures viewport screenshots and returns base64-encoded PNG data.
class_name StagehandScreenshotCapture
extends RefCounted

const SelectorEngine := preload("res://addons/stagehand/core/selector_engine.gd")


## Capture the viewport as a PNG base64 string.
## Optionally crops to a node's bounding rect if [param selector] is provided.
static func capture(tree: SceneTree, params: Dictionary) -> Dictionary:
	var viewport: Viewport = tree.root
	if viewport == null:
		return {"error": "No viewport available"}

	var full_page: bool = true
	var full_page_raw: Variant = params.get("full_page", true)
	if full_page_raw is bool:
		full_page = full_page_raw
	var crop_rect: Rect2i = Rect2i()

	if not full_page and params.has("selector"):
		var selector_str: String = str(params["selector"])
		var nodes: Array[Node] = SelectorEngine.query(tree, selector_str)
		if nodes.is_empty():
			return {"error": "Selector not found: %s" % selector_str}
		crop_rect = _get_node_rect(nodes[0])

	# Wait for the next process tick so the scene is in a stable state.
	# process_frame is guaranteed to fire; RenderingServer.frame_post_draw can
	# hang indefinitely in headless mode or when the renderer is stuck.
	await tree.process_frame

	var texture: ViewportTexture = viewport.get_texture()
	if texture == null:
		return {"error": "Viewport has no texture"}
	var img: Image = texture.get_image()
	# get_image() can return a non-null but ZERO-SIZE Image when the readback
	# target isn't ready. An empty image encodes to zero PNG bytes -> empty
	# base64 -> the client reports "empty image data". Reject it explicitly.
	if img == null or img.is_empty():
		return {"error": "Failed to capture viewport image (null or zero-size). The render target may not be ready, or this build has no rendered frame (headless/no GPU)."}

	if crop_rect != Rect2i():
		var clamped: Rect2i = Rect2i(
			Vector2i(
				clampi(crop_rect.position.x, 0, img.get_width()),
				clampi(crop_rect.position.y, 0, img.get_height())
			),
			Vector2i(
				mini(crop_rect.size.x, img.get_width() - crop_rect.position.x),
				mini(crop_rect.size.y, img.get_height() - crop_rect.position.y)
			)
		)
		if clamped.size.x > 0 and clamped.size.y > 0:
			img = img.get_region(clamped)

	var buffer: PackedByteArray = img.save_png_to_buffer()
	if buffer.is_empty():
		return {"error": "PNG encode produced zero bytes for a %dx%d image" % [img.get_width(), img.get_height()]}
	var base64: String = Marshalls.raw_to_base64(buffer)
	if base64.is_empty():
		return {"error": "base64 encode produced an empty string from %d PNG bytes" % buffer.size()}

	return {
		"data": base64,
		"mime_type": "image/png",
		"width": img.get_width(),
		"height": img.get_height(),
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
