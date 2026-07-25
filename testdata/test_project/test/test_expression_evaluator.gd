# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for StagehandExpressionEvaluator — engine-singleton resolution, the
## documented ternary (conditional expression) limitation, and context_node
## parameter handling (regression coverage for godot-stagehand-923: the
## context-node param is named "context_node"; a stale copy once read
## "selector", silently dropping the requested node and falling back to root).


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


func test_missing_expression_returns_error() -> void:
	var result: Dictionary = StagehandExpressionEvaluator.evaluate(get_tree(), {})
	var error_text: String = result.get("error", "")
	assert_bool(error_text.contains("Missing expression")).is_true()


func test_basic_expression_evaluates_without_context_node() -> void:
	var result: Dictionary = StagehandExpressionEvaluator.evaluate(
		get_tree(), {"expression": "1 + 1"}
	)
	assert_bool(result.has("error")).is_false()
	assert_that(result["value"]).is_equal(2)


func test_context_node_param_is_honored() -> void:
	# A node whose `self` context must be used when context_node points at it.
	var target: Node = auto_free(Node.new())
	target.name = "Stagehand923Target"
	add_child(target)

	var node_path: String = str(target.get_path())
	var result: Dictionary = StagehandExpressionEvaluator.evaluate(
		get_tree(), {"expression": "name", "context_node": node_path}
	)

	assert_bool(result.has("error")).is_false()
	# `self` is the target node, so `name` resolves to the target's name —
	# proving context_node was used rather than silently falling back to root.
	assert_that(str(result["value"])).is_equal("Stagehand923Target")


func test_unknown_context_node_returns_error() -> void:
	var result: Dictionary = StagehandExpressionEvaluator.evaluate(
		get_tree(),
		{"expression": "name", "context_node": "/root/NoSuchNode929391"}
	)
	var error_text: String = result.get("error", "")
	assert_bool(error_text.contains("context_node")).is_true()


func test_stale_selector_param_is_ignored() -> void:
	# The legacy "selector" key must NOT resolve a context node. If it did,
	# this would evaluate against the target; instead it falls back to root,
	# so the target's name is never returned.
	var target: Node = auto_free(Node.new())
	target.name = "Stagehand923Stale"
	add_child(target)

	var result: Dictionary = StagehandExpressionEvaluator.evaluate(
		get_tree(),
		{"expression": "name", "selector": str(target.get_path())}
	)

	assert_bool(result.has("error")).is_false()
	assert_bool(str(result["value"]) == "Stagehand923Stale").is_false()
