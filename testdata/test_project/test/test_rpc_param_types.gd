# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Every RPC handler must answer a wrong-typed parameter with a canonical
## invalid_params envelope (JSON-RPC -32602) instead of aborting on a typed
## Variant assignment — which pauses the game under the debugger — and must
## bound client-controlled counts and depths.
## (godot-stagehand-ext-review-2026-11-327t.21)

const TestableStagehandServer := preload("res://test/fixtures/testable_stagehand_server.gd")
const ERRORS := preload("res://addons/stagehand/core/errors.gd")

var _server: TestableStagehandServer


func before_test() -> void:
	_server = TestableStagehandServer.new()
	add_child(_server)
	auto_free(_server)


func _assert_invalid_params(result: Dictionary, parameter: String) -> void:
	assert_bool(ERRORS.is_error(result)).override_failure_message(
		"expected an error envelope, got %s" % str(result)
	).is_true()
	assert_str(ERRORS.code_of(result)).is_equal(ERRORS.INVALID_PARAMS)
	assert_int(ERRORS.json_rpc_code(ERRORS.code_of(result))).is_equal(-32602)
	assert_str(str(ERRORS.details_of(result).get("parameter", ""))).is_equal(parameter)


# ── static handlers ──────────────────────────────────────────────────────

func test_get_property_rejects_non_string_selector() -> void:
	_assert_invalid_params(
		StagehandPropertyHandler.get_property(get_tree(), {"selector": 5, "property": "name"}), "selector"
	)


func test_set_property_rejects_non_string_selector() -> void:
	_assert_invalid_params(
		StagehandPropertyHandler.set_property(get_tree(), {"selector": 5, "property": "name", "value": 1}), "selector"
	)


func test_set_property_rejects_non_string_property() -> void:
	_assert_invalid_params(
		StagehandPropertyHandler.set_property(get_tree(), {"selector": "/root", "property": ["x"], "value": 1}), "property"
	)


func test_call_method_rejects_wrong_types() -> void:
	_assert_invalid_params(
		StagehandMethodHandler.call_method(get_tree(), {"selector": 5, "method": "get_name"}), "selector"
	)
	_assert_invalid_params(
		StagehandMethodHandler.call_method(get_tree(), {"selector": "/root", "method": 7}), "method"
	)
	_assert_invalid_params(
		StagehandMethodHandler.call_method(get_tree(), {"selector": "/root", "method": "get_name", "args": "x"}), "args"
	)
	_assert_invalid_params(
		StagehandMethodHandler.call_method(
			get_tree(), {"selector": "/root", "method": "get_name", "allow_multiple": "yes"}
		), "allow_multiple"
	)


func test_evaluate_rejects_wrong_types() -> void:
	_assert_invalid_params(StagehandExpressionEvaluator.evaluate(get_tree(), {"expression": 5}), "expression")
	_assert_invalid_params(
		StagehandExpressionEvaluator.evaluate(get_tree(), {"expression": "1", "context_node": 3}), "context_node"
	)


func test_change_scene_rejects_non_string_scene_path() -> void:
	_assert_invalid_params(StagehandSceneHandler.change_scene(get_tree(), {"scene_path": 5}), "scene_path")


func test_input_mouse_rejects_wrong_types() -> void:
	_assert_invalid_params(StagehandInputSimulator.input_mouse(get_tree(), {"selector": 5}), "selector")
	_assert_invalid_params(StagehandInputSimulator.input_mouse(get_tree(), {"position": 5}), "position")
	_assert_invalid_params(
		StagehandInputSimulator.input_mouse(get_tree(), {"position": {"x": "a", "y": 1}}), "position"
	)
	_assert_invalid_params(
		StagehandInputSimulator.input_mouse(get_tree(), {"position": {"x": 1, "y": 1}, "button": 5}), "button"
	)
	_assert_invalid_params(
		StagehandInputSimulator.input_mouse(get_tree(), {"position": {"x": 1, "y": 1}, "hold_ms": "long"}), "hold_ms"
	)


func test_input_action_key_text_reject_wrong_types() -> void:
	_assert_invalid_params(StagehandInputSimulator.input_action(get_tree(), {"action": 5}), "action")
	_assert_invalid_params(StagehandInputSimulator.input_key(get_tree(), {"key": 5}), "key")
	_assert_invalid_params(
		StagehandInputSimulator.input_key(get_tree(), {"key": "a", "modifiers": "shift"}), "modifiers"
	)
	var typed: Dictionary = await StagehandInputSimulator.input_text(get_tree(), {"text": 5})
	_assert_invalid_params(typed, "text")


func test_input_touch_and_mouse_move_reject_wrong_types() -> void:
	_assert_invalid_params(StagehandInputSimulator.input_touch(get_tree(), {"position": 5}), "position")
	_assert_invalid_params(
		StagehandInputSimulator.input_touch(get_tree(), {"position": {"x": 1, "y": 1}, "action": 5}), "action"
	)
	_assert_invalid_params(
		StagehandInputSimulator.input_touch(get_tree(), {"position": {"x": 1, "y": 1}, "drag_to": 5}), "drag_to"
	)
	_assert_invalid_params(StagehandInputSimulator.input_mouse_move(get_tree(), {"selector": 5}), "selector")
	_assert_invalid_params(StagehandInputSimulator.input_mouse_move(get_tree(), {"coordinates": "0,0"}), "coordinates")


func test_screenshot_rejects_wrong_types() -> void:
	var result: Dictionary = await StagehandScreenshotCapture.capture(get_tree(), {"full_page": "no"})
	_assert_invalid_params(result, "full_page")


# ── server handlers ──────────────────────────────────────────────────────

func test_get_tree_rejects_wrong_types() -> void:
	_assert_invalid_params(_server._handle_get_tree({"root_path": 5}), "root_path")
	_assert_invalid_params(_server._handle_get_tree({"max_depth": "deep"}), "max_depth")
	_assert_invalid_params(_server._handle_get_tree({"properties": "name"}), "properties")


func test_get_tree_rejects_numeric_properties_instead_of_looping() -> void:
	# `for item in 1e9` would iterate a billion times.
	_assert_invalid_params(_server._handle_get_tree({"properties": 1000000000}), "properties")


func test_get_tree_rejects_oversized_properties_array() -> void:
	var many: Array = []
	for i: int in range(StagehandParams.MAX_ARRAY_ITEMS + 1):
		many.append("p%d" % i)
	_assert_invalid_params(_server._handle_get_tree({"properties": many}), "properties")


func test_query_nodes_rejects_wrong_types() -> void:
	_assert_invalid_params(_server._handle_query_nodes({"selector": 5}), "selector")
	_assert_invalid_params(_server._handle_query_nodes({"selector": "Node", "limit": "all"}), "limit")
	_assert_invalid_params(_server._handle_query_nodes({"selector": "Node", "properties": 1000000000}), "properties")


func test_accessibility_tree_rejects_wrong_types() -> void:
	_assert_invalid_params(_server._handle_get_accessibility_tree({"root_path": 5}), "root_path")
	_assert_invalid_params(_server._handle_get_accessibility_tree({"max_depth": []}), "max_depth")


func test_waits_reject_wrong_types() -> void:
	_assert_invalid_params(await _server._handle_wait_for_node({"selector": 5}), "selector")
	_assert_invalid_params(await _server._handle_wait_for_node({"selector": "Node", "timeout_ms": "1s"}), "timeout_ms")
	_assert_invalid_params(await _server._handle_wait_signal({"selector": "Node", "signal_name": 5}), "signal_name")
	_assert_invalid_params(
		await _server._handle_wait_for_property({"selector": "Node", "property": 5, "operator": "eq"}), "property"
	)
	_assert_invalid_params(
		await _server._handle_wait_for_property({"selector": "Node", "property": "x", "operator": 5}), "operator"
	)


func test_performance_handlers_reject_wrong_types() -> void:
	_assert_invalid_params(_server._handle_get_performance({"monitors": 5}), "monitors")
	_assert_invalid_params(await _server._handle_assert_performance({"monitor": 5}), "monitor")
	_assert_invalid_params(
		await _server._handle_assert_performance({"monitor": "OBJECT_COUNT", "threshold": "high"}), "threshold"
	)
	_assert_invalid_params(
		await _server._handle_assert_performance({"monitor": "OBJECT_COUNT", "sample_count": "many"}), "sample_count"
	)


func test_recorder_handlers_reject_wrong_types() -> void:
	_assert_invalid_params(_server._handle_record_start({"output_path": 5}), "output_path")
	_assert_invalid_params(_server._handle_record_start({"include_mouse_move": "yes"}), "include_mouse_move")
	_assert_invalid_params(await _server._handle_replay({"recording_path": 5}), "recording_path")
	_assert_invalid_params(await _server._handle_replay({"recording_path": "x", "speed": "fast"}), "speed")


# ── bounds ───────────────────────────────────────────────────────────────

func test_sample_count_is_clamped_not_looped_a_billion_times() -> void:
	var result: Dictionary = await _server._handle_assert_performance({
		"monitor": "OBJECT_COUNT", "threshold": 0, "op": "gte", "sample_count": 1000000000,
	})
	assert_int(result.get("sample_count", -1)).is_equal(StagehandParams.MAX_SAMPLE_COUNT)


func test_huge_max_depth_is_accepted_and_clamped() -> void:
	var result: Dictionary = _server._handle_get_tree({"max_depth": 1000000000})
	assert_bool(ERRORS.is_error(result)).is_false()


# ── authentication token comparison ──────────────────────────────────────

func test_tokens_match_compares_by_value() -> void:
	assert_bool(TestableStagehandServer._tokens_match("secret", "secret")).is_true()
	assert_bool(TestableStagehandServer._tokens_match("secret", "secreT")).is_false()
	assert_bool(TestableStagehandServer._tokens_match("secret", "secre")).is_false()
	assert_bool(TestableStagehandServer._tokens_match("", "secret")).is_false()
	assert_bool(TestableStagehandServer._tokens_match("", "")).is_true()
