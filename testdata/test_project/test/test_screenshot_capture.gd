# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for StagehandScreenshotCapture error diagnostics.


## A scripted renderer. --headless never draws, so everything that decides
## whether a capture is fresh is driven from here. Every engine frame advances
## the draw count by [member draws_per_frame], and the render target only takes
## the new pose when [member root_redraws] is true.
class ScriptedRender:
	extends StagehandScreenshotCapture.RenderView

	var frames_drawn_now: int = 100
	var draws_per_frame: int = 1
	var root_redraws: bool = true
	var drawable: bool = true
	var idle_and_current: bool = false
	var window_mode: int = DisplayServer.WINDOW_MODE_MAXIMIZED
	var empty_images: bool = false
	var pose: int = 0
	var shown_pose: int = 0
	var draws_holding_old_image: int = 0

	func tick() -> void:
		frames_drawn_now += draws_per_frame
		if draws_holding_old_image > 0:
			draws_holding_old_image -= 1
			return
		if root_redraws:
			shown_pose = pose

	func read_image(_viewport: Viewport) -> Image:
		if empty_images:
			return null
		var img: Image = Image.create(4, 4, false, Image.FORMAT_RGB8)
		img.fill(Color8(shown_pose, 0, 0))
		return img

	func frames_drawn() -> int:
		return frames_drawn_now

	func can_draw(_window: Window) -> bool:
		return drawable

	func window_mode_of(_window: Window) -> int:
		return window_mode

	func is_idle_and_current() -> bool:
		return idle_and_current

	func camera_pose(_viewport: Viewport) -> PackedByteArray:
		return var_to_bytes(pose)


func before_test() -> void:
	StagehandScreenshotCapture._previous_capture = {}


## A capture's red channel names the pose its render target was showing.
func _shown_pose(result: Dictionary) -> int:
	var raw: String = result["data"]
	var png: PackedByteArray = Marshalls.base64_to_raw(raw)
	var img: Image = Image.new()
	var err: Error = img.load_png_from_buffer(png)
	assert_int(err).is_equal(OK)
	return roundi(img.get_pixel(0, 0).r * 255.0)


func _capture(render: ScriptedRender) -> Dictionary:
	var tree: SceneTree = get_tree()
	var err: int = tree.process_frame.connect(render.tick)
	assert_int(err).is_equal(OK)
	var result: Dictionary = await StagehandScreenshotCapture.capture(tree, {}, render)
	tree.process_frame.disconnect(render.tick)
	return result


func test_one_draw_after_the_request_is_not_enough() -> void:
	var render: ScriptedRender = ScriptedRender.new()
	render.pose = 9
	render.draws_holding_old_image = 1

	var result: Dictionary = await _capture(render)

	assert_int(_shown_pose(result)).is_equal(9)


func test_capture_reports_the_frame_it_read_and_the_draws_it_waited_for() -> void:
	var render: ScriptedRender = ScriptedRender.new()

	var result: Dictionary = await _capture(render)

	assert_bool(result.has("error")).is_false()
	assert_int(result.get("fresh_draws", 0)).is_greater_equal(
		StagehandScreenshotCapture.FRESH_DRAWS_REQUIRED
	)
	assert_int(result.get("frame", 0)).is_equal(render.frames_drawn_now)


func test_camera_move_is_not_answered_with_the_previous_image() -> void:
	var render: ScriptedRender = ScriptedRender.new()
	var first: Dictionary = await _capture(render)
	assert_int(_shown_pose(first)).is_equal(0)

	# The ticket's shape: the camera moves, draws keep landing, and the render
	# target still holds the previous image for a few more reads.
	render.pose = 9
	render.draws_holding_old_image = 3
	var second: Dictionary = await _capture(render)

	assert_int(_shown_pose(second)).is_equal(9)
	assert_bool(second.has("unchanged_after_camera_move")).is_false()


func test_image_that_never_changes_across_a_camera_move_is_flagged() -> void:
	var render: ScriptedRender = ScriptedRender.new()
	var _first: Dictionary = await _capture(render)

	# A full-screen menu over a moving 3D camera looks exactly like this, so the
	# image still comes back, but the caller is told.
	render.pose = 9
	render.root_redraws = false
	var second: Dictionary = await _capture(render)

	assert_bool(second.has("error")).is_false()
	assert_bool(second.get("unchanged_after_camera_move", false)).is_true()


func test_retry_of_a_flagged_capture_stays_flagged_while_the_image_is_stuck() -> void:
	var render: ScriptedRender = ScriptedRender.new()
	var _first: Dictionary = await _capture(render)
	render.pose = 9
	render.root_redraws = false
	var flagged: Dictionary = await _capture(render)
	assert_bool(flagged.get("unchanged_after_camera_move", false)).is_true()

	# The warning tells the caller to capture again, so that retry must not
	# launder the same image into an unflagged result.
	var retry: Dictionary = await _capture(render)

	assert_bool(retry.get("unchanged_after_camera_move", false)).is_true()


func test_identical_image_without_a_camera_move_is_not_flagged() -> void:
	var render: ScriptedRender = ScriptedRender.new()
	var _first: Dictionary = await _capture(render)

	var second: Dictionary = await _capture(render)

	assert_bool(second.has("error")).is_false()
	assert_bool(second.has("unchanged_after_camera_move")).is_false()


func test_undrawable_window_is_an_error_even_while_frames_drawn_advances() -> void:
	# Measured on the Windows GUI build: a minimized game crept ~127 frames in
	# two minutes while every capture came back byte-identical.
	var render: ScriptedRender = ScriptedRender.new()
	render.drawable = false
	render.window_mode = DisplayServer.WINDOW_MODE_MINIMIZED

	var result: Dictionary = await _capture(render)

	assert_that(result.get("error_code")).is_equal("stale_frame")
	assert_bool(result.has("data")).is_false()
	var details: Dictionary = result["details"]
	assert_that(details.get("window_mode")).is_equal(DisplayServer.WINDOW_MODE_MINIMIZED)
	assert_bool(details.has("next_action")).is_true()


func test_no_draw_at_all_is_an_error_rather_than_the_held_image() -> void:
	var render: ScriptedRender = ScriptedRender.new()
	render.draws_per_frame = 0

	var result: Dictionary = await _capture(render)

	assert_that(result.get("error_code")).is_equal("stale_frame")
	var details: Dictionary = result["details"]
	assert_that(details.get("frames_drawn")).is_equal(0)


func test_idle_low_processor_game_returns_its_current_image() -> void:
	# low_processor_usage_mode stops drawing when nothing changed, so a missing
	# draw there means the held image IS the current one.
	var render: ScriptedRender = ScriptedRender.new()
	render.draws_per_frame = 0
	render.idle_and_current = true

	var result: Dictionary = await _capture(render)

	assert_bool(result.has("error")).is_false()
	assert_int(result.get("fresh_draws", -1)).is_equal(0)


func test_no_pixels_stays_renderer_unavailable_so_visual_checks_skip() -> void:
	var render: ScriptedRender = ScriptedRender.new()
	render.drawable = false
	render.draws_per_frame = 0
	render.empty_images = true

	var result: Dictionary = await _capture(render)

	assert_that(result.get("error_code")).is_equal("renderer_unavailable")


func test_stale_read_does_not_become_the_previous_capture() -> void:
	var render: ScriptedRender = ScriptedRender.new()
	var _first: Dictionary = await _capture(render)

	render.drawable = false
	render.root_redraws = false
	render.pose = 9
	var refused: Dictionary = await _capture(render)
	assert_that(refused.get("error_code")).is_equal("stale_frame")

	render.drawable = true
	var third: Dictionary = await _capture(render)

	assert_bool(third.get("unchanged_after_camera_move", false)).is_true()


func test_real_headless_capture_is_still_renderer_unavailable() -> void:
	var result: Dictionary = await StagehandScreenshotCapture.capture(get_tree(), {})

	assert_that(result.get("error_code")).is_equal("renderer_unavailable")


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
