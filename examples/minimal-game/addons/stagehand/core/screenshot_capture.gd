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

	var full_page: bool = params.get("full_page", true)
	var crop_rect: Rect2i = Rect2i()

	if not full_page and params.has("selector"):
		var nodes: Array[Node] = SelectorEngine.query(tree, str(params["selector"]))
		if not nodes.is_empty():
			var node: Node = nodes[0]
			crop_rect = _get_node_rect(node)

	# Wait for the frame to be fully rendered.
	await RenderingServer.frame_post_draw

	var img: Image = viewport.get_texture().get_image()
	if img == null:
		return {"error": "Failed to capture viewport image"}

	if crop_rect != Rect2i():
		var clamped := Rect2i(
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
	var base64: String = Marshalls.raw_to_base64(buffer)

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
