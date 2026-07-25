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

	assert_that(result.get("error_code")).is_equal("crop_outside_viewport")
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
