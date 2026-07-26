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
