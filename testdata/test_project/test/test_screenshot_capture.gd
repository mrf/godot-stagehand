# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for StagehandScreenshotCapture error diagnostics.


func test_crop_rect_outside_viewport_returns_structured_error() -> void:
	var result: Dictionary = StagehandScreenshotCapture._validate_crop_rect(
		Rect2i(Vector2i(1280, 0), Vector2i(100, 100)),
		Vector2i(640, 480)
	)

	assert_that(result.get("error_code")).is_equal("invalid_params")
	assert_bool(result.has("details")).is_true()
	var details: Dictionary = result["details"]
	assert_that(details.get("viewport_width")).is_equal(640)
	assert_that(details.get("viewport_height")).is_equal(480)


func test_crop_rect_partially_inside_viewport_returns_intersection() -> void:
	var result: Dictionary = StagehandScreenshotCapture._validate_crop_rect(
		Rect2i(Vector2i(600, 440), Vector2i(100, 100)),
		Vector2i(640, 480)
	)

	assert_bool(result.has("error")).is_false()
	var rect: Rect2i = result["rect"]
	assert_that(rect.position).is_equal(Vector2i(600, 440))
	assert_that(rect.size).is_equal(Vector2i(40, 40))


func test_get_node_rect_returns_error_for_unsupported_node_type() -> void:
	var node: Node = auto_free(Node.new())

	var result: Dictionary = StagehandScreenshotCapture._get_node_rect(node)

	assert_that(result.get("error_code")).is_equal("invalid_params")
	assert_bool(result.has("details")).is_true()
	var details: Dictionary = result["details"]
	assert_that(details.get("node_class")).is_equal("Node")


func test_get_node_rect_returns_error_for_zero_sized_control() -> void:
	var control: Control = auto_free(Control.new())
	control.position = Vector2.ZERO
	control.size = Vector2.ZERO

	var result: Dictionary = StagehandScreenshotCapture._get_node_rect(control)

	assert_that(result.get("error_code")).is_equal("invalid_params")
	assert_bool(result.has("details")).is_true()


func test_get_node_rect_returns_rect_for_sized_control() -> void:
	var control: Control = auto_free(Control.new())
	control.position = Vector2(10, 20)
	control.size = Vector2(30, 40)

	var result: Dictionary = StagehandScreenshotCapture._get_node_rect(control)

	assert_bool(result.has("error")).is_false()
	var rect: Rect2i = result["rect"]
	assert_that(rect.position).is_equal(Vector2i(10, 20))
	assert_that(rect.size).is_equal(Vector2i(30, 40))


func test_screen_rect_applies_camera_canvas_offset() -> void:
	# Camera2D scrolled by (500, 0) puts a world-space node at x=600 on screen at x=100.
	var canvas_xform: Transform2D = Transform2D(0.0, Vector2(-500, 0))

	var rect: Rect2i = StagehandScreenshotCapture._screen_rect(
		Rect2(Vector2(0, 0), Vector2(40, 30)), canvas_xform * Transform2D(0.0, Vector2(600, 50)), Transform2D.IDENTITY
	)

	assert_that(rect).is_equal(Rect2i(Vector2i(100, 50), Vector2i(40, 30)))


func test_screen_rect_applies_stretch_transform() -> void:
	var stretch: Transform2D = Transform2D().scaled(Vector2(2, 2))

	var rect: Rect2i = StagehandScreenshotCapture._screen_rect(
		Rect2(Vector2(0, 0), Vector2(40, 30)), Transform2D(0.0, Vector2(10, 20)), stretch
	)

	assert_that(rect).is_equal(Rect2i(Vector2i(20, 40), Vector2i(80, 60)))


func test_screen_rect_uses_bounding_box_of_rotated_corners() -> void:
	var rect: Rect2i = StagehandScreenshotCapture._screen_rect(
		Rect2(Vector2(0, 0), Vector2(10, 20)), Transform2D(PI / 2.0, Vector2.ZERO), Transform2D.IDENTITY
	)

	assert_that(rect).is_equal(Rect2i(Vector2i(-20, 0), Vector2i(20, 10)))


func test_get_node_rect_node2d_honors_camera_canvas_transform() -> void:
	var host: Node2D = auto_free(Node2D.new())
	add_child(host)
	var camera: Camera2D = Camera2D.new()
	camera.anchor_mode = Camera2D.ANCHOR_MODE_FIXED_TOP_LEFT
	camera.position = Vector2(500, 0)
	host.add_child(camera)
	camera.make_current()
	var player: Node2D = Node2D.new()
	player.position = Vector2(600, 50)
	var sprite: Sprite2D = Sprite2D.new()
	sprite.name = "Sprite2D"
	sprite.texture = _solid_texture(Vector2i(40, 30))
	player.add_child(sprite)
	host.add_child(player)
	await get_tree().process_frame
	await get_tree().process_frame

	var result: Dictionary = StagehandScreenshotCapture._get_node_rect(player)

	assert_bool(result.has("error_code")).is_false()
	var rect: Rect2i = result["rect"]
	# Sprite is centered on the node: node on screen at (100, 50) -> (80, 35)..(120, 65).
	assert_that(rect).is_equal(Rect2i(Vector2i(80, 35), Vector2i(40, 30)))
	camera.queue_free()


func test_get_node_rect_node2d_honors_canvas_layer_offset() -> void:
	var layer: CanvasLayer = auto_free(CanvasLayer.new())
	layer.offset = Vector2(300, 100)
	add_child(layer)
	var marker: Node2D = Node2D.new()
	marker.position = Vector2(20, 10)
	var sprite: Sprite2D = Sprite2D.new()
	sprite.name = "Sprite2D"
	sprite.texture = _solid_texture(Vector2i(40, 30))
	marker.add_child(sprite)
	layer.add_child(marker)
	await get_tree().process_frame

	var result: Dictionary = StagehandScreenshotCapture._get_node_rect(marker)

	assert_bool(result.has("error_code")).is_false()
	var rect: Rect2i = result["rect"]
	assert_that(rect).is_equal(Rect2i(Vector2i(300, 95), Vector2i(40, 30)))


func test_get_node_rect_control_honors_canvas_layer_offset() -> void:
	var layer: CanvasLayer = auto_free(CanvasLayer.new())
	layer.offset = Vector2(300, 100)
	add_child(layer)
	var control: Control = Control.new()
	control.position = Vector2(10, 20)
	control.size = Vector2(30, 40)
	layer.add_child(control)
	await get_tree().process_frame

	var result: Dictionary = StagehandScreenshotCapture._get_node_rect(control)

	var rect: Rect2i = result["rect"]
	assert_that(rect).is_equal(Rect2i(Vector2i(310, 120), Vector2i(30, 40)))


func _solid_texture(size: Vector2i) -> ImageTexture:
	return ImageTexture.create_from_image(Image.create(size.x, size.y, false, Image.FORMAT_RGBA8))
