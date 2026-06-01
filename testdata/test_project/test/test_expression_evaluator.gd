extends GdUnitTestSuite
## Tests for StagehandExpressionEvaluator — GDScript expression evaluation.
##
## Regression coverage for godot-stagehand-923: the context-node parameter
## sent by the Go server is named "context_node". A stale copy once read
## "selector", silently dropping the requested node and falling back to
## tree.root. These tests pin the param name and the honored-context behavior.


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
