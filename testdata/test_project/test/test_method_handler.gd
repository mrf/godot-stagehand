extends GdUnitTestSuite
## Tests for StagehandMethodHandler — call_method dispatch and the
## allow_multiple multi-match contract (results array).


## Minimal node with a public method, used as a call target.
class _Doubler:
	extends Node
	func double(n: int) -> int:
		return n * 2


func _make_target(group: StringName) -> void:
	var node: _Doubler = _Doubler.new()
	add_child(auto_free(node))
	node.add_to_group(group)


func test_single_match_returns_return_value() -> void:
	_make_target(&"doublers_single")
	var params: Dictionary = {
		"selector": "group:doublers_single",
		"method": "double",
		"args": [5],
	}
	var result: Dictionary = StagehandMethodHandler.call_method(get_tree(), params)

	assert_bool(result.get("success", false)).is_true()
	assert_that(result.get("return_value")).is_equal(10)
	assert_bool(result.has("results")).is_false()


func test_allow_multiple_returns_results_array() -> void:
	_make_target(&"doublers_multi")
	_make_target(&"doublers_multi")
	_make_target(&"doublers_multi")
	var params: Dictionary = {
		"selector": "group:doublers_multi",
		"method": "double",
		"args": [7],
		"allow_multiple": true,
	}
	var result: Dictionary = StagehandMethodHandler.call_method(get_tree(), params)

	assert_bool(result.get("success", false)).is_true()
	assert_bool(result.has("return_value")).is_false()

	var results: Array = result.get("results", [])
	assert_int(results.size()).is_equal(3)
	for entry: Variant in results:
		var dict: Dictionary = entry
		assert_that(dict.get("return_value")).is_equal(14)
		assert_str(dict.get("node_path", "")).is_not_empty()


func test_allow_multiple_false_defaults_to_single() -> void:
	_make_target(&"doublers_default")
	_make_target(&"doublers_default")
	var params: Dictionary = {
		"selector": "group:doublers_default",
		"method": "double",
		"args": [3],
	}
	var result: Dictionary = StagehandMethodHandler.call_method(get_tree(), params)

	assert_bool(result.has("results")).is_false()
	assert_that(result.get("return_value")).is_equal(6)


func test_missing_selector_returns_error() -> void:
	var result: Dictionary = StagehandMethodHandler.call_method(get_tree(), {"method": "double"})
	assert_str(result.get("error", "")).is_not_empty()


func test_blocked_method_returns_error() -> void:
	_make_target(&"doublers_blocked")
	var params: Dictionary = {
		"selector": "group:doublers_blocked",
		"method": "queue_free",
	}
	var result: Dictionary = StagehandMethodHandler.call_method(get_tree(), params)
	assert_str(result.get("error", "")).contains("destructive")
