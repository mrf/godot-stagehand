# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for StagehandMethodHandler — call_method dispatch and the
## allow_multiple multi-match contract (results array).


## Minimal node with a public method, used as a call target.
class _Doubler:
	extends Node
	func double(n: int) -> int:
		return n * 2


func _make_target(group: StringName) -> void:
	var node: _Doubler = auto_free(_Doubler.new())
	add_child(node)
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


# ── safety boundary: blocklist and private methods ───────────────────────

func test_every_blocked_method_is_rejected() -> void:
	_make_target(&"doublers_blocklist")
	for method: String in StagehandMethodHandler.BLOCKED_METHODS:
		var result: Dictionary = StagehandMethodHandler.call_method(get_tree(), {
			"selector": "group:doublers_blocklist",
			"method": method,
		})
		assert_str(str(result.get("error", ""))) \
			.override_failure_message("expected '%s' to be blocked" % method) \
			.contains("destructive")


func test_blocklist_covers_the_destructive_lifecycle_methods() -> void:
	# Guards against a future edit silently shrinking the blocklist.
	for method: String in ["free", "queue_free", "set_script", "add_child", "remove_child"]:
		assert_bool(method in StagehandMethodHandler.BLOCKED_METHODS) \
			.override_failure_message("'%s' must stay on the blocklist" % method) \
			.is_true()


func test_private_method_is_rejected() -> void:
	_make_target(&"doublers_private")
	var result: Dictionary = StagehandMethodHandler.call_method(get_tree(), {
		"selector": "group:doublers_private",
		"method": "_ready",
	})
	assert_str(str(result.get("error", ""))).contains("private/lifecycle")


func test_private_method_rejected_before_node_lookup() -> void:
	# The name check must run first, so a private method is refused even when
	# the selector matches nothing — never leaking which nodes exist.
	var result: Dictionary = StagehandMethodHandler.call_method(get_tree(), {
		"selector": "group:no_such_group_at_all",
		"method": "_enter_tree",
	})
	assert_str(str(result.get("error", ""))).contains("private/lifecycle")


func test_blocked_method_rejected_before_node_lookup() -> void:
	var result: Dictionary = StagehandMethodHandler.call_method(get_tree(), {
		"selector": "group:no_such_group_at_all",
		"method": "queue_free",
	})
	assert_str(str(result.get("error", ""))).contains("destructive")


# ── allowed methods ──────────────────────────────────────────────────────

func test_allowed_builtin_method_is_callable() -> void:
	# A non-destructive engine method must pass the safety check.
	_make_target(&"doublers_builtin")
	var result: Dictionary = StagehandMethodHandler.call_method(get_tree(), {
		"selector": "group:doublers_builtin",
		"method": "get_class",
	})
	assert_bool(result.get("success", false)).is_true()


func test_method_with_no_args_is_callable() -> void:
	_make_target(&"doublers_noargs")
	var result: Dictionary = StagehandMethodHandler.call_method(get_tree(), {
		"selector": "group:doublers_noargs",
		"method": "is_inside_tree",
	})
	assert_bool(result.get("success", false)).is_true()
	assert_bool(result.get("return_value", false)).is_true()


# ── remaining error cases ────────────────────────────────────────────────

func test_missing_method_name_returns_error() -> void:
	var result: Dictionary = StagehandMethodHandler.call_method(
		get_tree(), {"selector": "group:doublers_single"}
	)
	assert_str(str(result.get("error", ""))).contains("Missing required parameter: method")


func test_unmatched_selector_returns_node_not_found() -> void:
	var result: Dictionary = StagehandMethodHandler.call_method(get_tree(), {
		"selector": "group:no_such_group_at_all",
		"method": "double",
		"args": [1],
	})
	assert_str(str(result.get("error", ""))).contains("Node not found")


func test_unknown_method_on_matched_node_returns_error() -> void:
	_make_target(&"doublers_unknown")
	var result: Dictionary = StagehandMethodHandler.call_method(get_tree(), {
		"selector": "group:doublers_unknown",
		"method": "no_such_method_at_all",
	})
	assert_str(str(result.get("error", ""))).contains("Method not found")
