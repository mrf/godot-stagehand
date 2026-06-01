extends GdUnitTestSuite
## Tests for StagehandExpressionEvaluator — engine-singleton resolution and the
## documented ternary (conditional expression) limitation.


func test_engine_singleton_resolves() -> void:
	var result: Dictionary = StagehandExpressionEvaluator.evaluate(
		get_tree(), {"expression": "Engine.get_physics_frames()"}
	)

	assert_bool(result.has("error")).is_false()
	assert_that(result.get("type")).is_equal("int")
	assert_int(result.get("value")).is_greater_equal(0)


func test_os_singleton_resolves() -> void:
	var result: Dictionary = StagehandExpressionEvaluator.evaluate(
		get_tree(), {"expression": "OS.get_name()"}
	)

	assert_bool(result.has("error")).is_false()
	assert_that(result.get("type")).is_equal("String")
	assert_str(result.get("value")).is_not_empty()


func test_time_singleton_resolves() -> void:
	var result: Dictionary = StagehandExpressionEvaluator.evaluate(
		get_tree(), {"expression": "Time.get_ticks_msec()"}
	)

	assert_bool(result.has("error")).is_false()
	assert_that(result.get("type")).is_equal("int")


func test_project_settings_singleton_resolves() -> void:
	var result: Dictionary = StagehandExpressionEvaluator.evaluate(
		get_tree(), {"expression": "ProjectSettings.has_setting(\"application/config/name\")"}
	)

	assert_bool(result.has("error")).is_false()
	assert_that(result.get("type")).is_equal("bool")


func test_display_server_singleton_resolves() -> void:
	var result: Dictionary = StagehandExpressionEvaluator.evaluate(
		get_tree(), {"expression": "DisplayServer.get_name()"}
	)

	assert_bool(result.has("error")).is_false()
	assert_that(result.get("type")).is_equal("String")


func test_input_singleton_resolves() -> void:
	# Input.is_anything_pressed() is safe to call headless and returns a bool.
	var result: Dictionary = StagehandExpressionEvaluator.evaluate(
		get_tree(), {"expression": "Input.is_anything_pressed()"}
	)

	assert_bool(result.has("error")).is_false()
	assert_that(result.get("type")).is_equal("bool")


func test_singleton_binding_does_not_break_plain_expressions() -> void:
	# Binding singletons as inputs must not interfere with ordinary arithmetic.
	var result: Dictionary = StagehandExpressionEvaluator.evaluate(
		get_tree(), {"expression": "2 + 3 * 4"}
	)

	assert_bool(result.has("error")).is_false()
	assert_int(result.get("value")).is_equal(14)


func test_ternary_in_call_argument_is_rejected_with_parse_error() -> void:
	# DOCUMENTED LIMITATION: Godot's Expression class cannot parse conditional
	# expressions inside a call argument. We assert the engine's parse error is
	# surfaced (rather than silently mis-evaluating) so the limitation stays
	# visible if a future Godot version changes this behavior.
	var result: Dictionary = StagehandExpressionEvaluator.evaluate(
		get_tree(), {"expression": "str(1 if true else 2)"}
	)

	assert_bool(result.has("error")).is_true()
	assert_str(result.get("error")).starts_with("Parse error")
