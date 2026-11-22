extends Node


const ERRORS := preload("res://addons/stagehand/core/errors.gd")
const SELECTOR_ENGINE := preload("res://addons/stagehand/core/selector_engine.gd")
const SELF_PROTECTION := preload("res://addons/stagehand/core/self_protection.gd")

## Widest signal wait_for_signal can capture; see _make_signal_capture.
const MAX_SIGNAL_ARGS: int = 8


## Polling mechanism that waits for a condition to become true.
## Returns true if condition was met within timeout, false otherwise.
func wait_condition(condition_func: Callable, timeout_ms: int = 10000, poll_interval_ms: int = 100) -> bool:
	var start_time: int = Time.get_ticks_msec()
	var end_time: int = start_time + timeout_ms

	while Time.get_ticks_msec() < end_time:
		if condition_func.call():
			return true
		await get_tree().create_timer(poll_interval_ms * 0.001, true, false, true).timeout

	if condition_func.call():
		return true

	return false


## Evaluate different types of property conditions. Reads a property, a
## `.`/`:` sub-path, or metadata — never a get_<name>() method, which could run
## arbitrary game code in safe mode.
func evaluate_property_condition(node: Node, property_name: String, operator: String, expected_value: Variant) -> bool:
	var actual_value: Variant = null
	if property_name in node:
		actual_value = node.get(property_name)
	elif property_name.contains(".") or property_name.contains(":"):
		actual_value = node.get_indexed(NodePath(property_name.replace(".", ":")))
		if actual_value == null:
			return false
	elif node.has_meta(property_name):
		actual_value = node.get_meta(property_name)
	else:
		return false

	match operator:
		"equals":
			return actual_value == _parse_stringified_expected(expected_value, actual_value)
		"not_equals":
			return actual_value != _parse_stringified_expected(expected_value, actual_value)
		"exists":
			return actual_value != null
		"contains":
			if actual_value is String or actual_value is StringName:
				if expected_value is String or expected_value is StringName:
					var s: String = actual_value
					var e: String = expected_value
					return s.contains(e)
			elif actual_value is Array:
				var a: Array = actual_value
				return a.has(expected_value)
			elif actual_value is Dictionary:
				var d: Dictionary = actual_value
				return d.has(expected_value)
			return false
		"greater_than":
			var parsed_expected: Variant = _parse_stringified_expected(expected_value, actual_value)
			var actual_f: float = _to_float(actual_value)
			var expected_f: float = _to_float(parsed_expected)
			if (actual_value is float or actual_value is int) and (parsed_expected is float or parsed_expected is int):
				return actual_f > expected_f
			return false
		"less_than":
			var parsed_expected: Variant = _parse_stringified_expected(expected_value, actual_value)
			var actual_f: float = _to_float(actual_value)
			var expected_f: float = _to_float(parsed_expected)
			if (actual_value is float or actual_value is int) and (parsed_expected is float or parsed_expected is int):
				return actual_f < expected_f
			return false
		_:
			push_error("Unknown operator: " + operator)
			return false


## Wait for a node matching the selector to reach the desired state.
## state: "exists" (node in tree), "visible" (visible in tree: the node and every ancestor visible), "removed" (node absent)
func wait_for_node(selector: String, state: String = "exists", timeout_ms: int = 10000, poll_interval_ms: int = 100) -> Dictionary:
	var condition: Callable = func() -> bool:
		var results: Array[Node] = SELECTOR_ENGINE.query(get_tree(), selector)
		match state:
			"exists":
				return results.size() > 0
			"visible":
				if results.size() > 0 and results[0] is CanvasItem:
					var ci: CanvasItem = results[0]
					return ci.is_visible_in_tree()
				return false
			"removed":
				return results.size() == 0
			_:
				return results.size() > 0

	var success: bool = await wait_condition(condition, timeout_ms, poll_interval_ms)

	if success:
		match state:
			"removed":
				return {"success": true, "removed": true, "message": "Node removed within timeout period"}
			"visible":
				return {"success": true, "found": true, "visible": true, "message": "Node found and visible within timeout period"}
			_:
				return {"success": true, "found": true, "message": "Node found within timeout period"}
	else:
		var message: String = "Node did not appear before timeout"
		match state:
			"removed":
				message = "Node did not disappear before timeout"
			"visible":
				message = "Node did not become visible before timeout"
		return ERRORS.make(
			ERRORS.TIMEOUT,
			"%s (selector: %s, timeout: %dms)" % [message, selector, timeout_ms],
			{
				"selector": selector,
				"state": state,
				"timeout_ms": timeout_ms,
				"next_action": "Raise timeout_ms, or call query_nodes to confirm the selector matches the node you expect.",
			}
		)


## Wait for a signal to be emitted on a node.
## Uses one-shot signal connection with timer-based timeout (no polling).
func wait_for_signal(selector: String, signal_name: String, timeout_ms: int = 5000) -> Dictionary:
	var start_time: int = Time.get_ticks_msec()

	var results: Array[Node] = SELECTOR_ENGINE.query(get_tree(), selector)
	if results.is_empty():
		return ERRORS.node_not_found(selector)

	var node: Node = results[0]

	if not node.has_signal(signal_name):
		return ERRORS.make(
			ERRORS.INVALID_PARAMS,
			"Signal '%s' not found on node '%s'" % [signal_name, node.get_path()],
			{
				"selector": selector,
				"node_path": str(node.get_path()),
				"signal_name": signal_name,
				"next_action": "Check the node class documentation, or its script, for the signal's exact name.",
			}
		)

	var argc: int = _signal_arg_count(node, signal_name)
	if argc > MAX_SIGNAL_ARGS:
		return ERRORS.make(
			ERRORS.INVALID_PARAMS,
			"Signal '%s' on node '%s' declares %d arguments; wait_for_signal captures at most %d"
				% [signal_name, node.get_path(), argc, MAX_SIGNAL_ARGS],
			{
				"selector": selector,
				"signal_name": signal_name,
				"argument_count": argc,
				"max_arguments": MAX_SIGNAL_ARGS,
				"next_action": "Wait on a signal with fewer arguments, or poll the state it changes with wait_for_property.",
			}
		)

	# Shared by reference with the capture lambda: [received, signal_args].
	var state: Array = [false, []]
	var callback: Callable = _make_signal_capture(state, argc)

	if not is_instance_valid(node):
		return _node_freed(selector, "before signal connection", Time.get_ticks_msec() - start_time)

	var _err: int = node.connect(signal_name, callback, CONNECT_ONE_SHOT)

	var end_time: int = start_time + timeout_ms
	while not state[0] and Time.get_ticks_msec() < end_time:
		if not is_instance_valid(node):
			return _node_freed(selector, "while waiting for signal", Time.get_ticks_msec() - start_time)
		await get_tree().create_timer(0.01, true, false, true).timeout

	var elapsed: int = Time.get_ticks_msec() - start_time

	if state[0]:
		return {"received": true, "elapsed_ms": elapsed, "args": state[1]}

	if is_instance_valid(node) and node.is_connected(signal_name, callback):
		node.disconnect(signal_name, callback)

	# A signal that never arrived is a failure, not a success carrying
	# `received: false` — reporting it as a normal result made a timed-out wait
	# indistinguishable from a satisfied one at the MCP layer (godot-stagehand-vv2.8).
	return ERRORS.make(
		ERRORS.TIMEOUT,
		"Signal '%s' was not emitted before timeout (selector: %s, timeout: %dms)"
			% [signal_name, selector, timeout_ms],
		{
			"selector": selector,
			"signal_name": signal_name,
			"timeout_ms": timeout_ms,
			"elapsed_ms": elapsed,
			"next_action": "Raise timeout_ms, or drive the game state that emits this signal before waiting on it.",
		}
	)


## Wait for a node's property to satisfy a condition.
func wait_for_property(selector: String, property: String, operator: String, expected_value: Variant, timeout_ms: int = 10000, poll_interval_ms: int = 100) -> Dictionary:
	if SELF_PROTECTION.is_forbidden_property(property):
		return SELF_PROTECTION.forbidden_property_error(selector, property)
	# A condition on Stagehand's own node is a read oracle for its private state
	# (godot-stagehand-ext-review-2026-11-327t.1): refuse it up front, and never
	# let a match that only resolves to one later satisfy the wait.
	var initial: Array[Node] = SELECTOR_ENGINE.query(get_tree(), selector)
	if not initial.is_empty() and SELF_PROTECTION.is_protected(initial[0]):
		return SELF_PROTECTION.protected_node_error(selector, initial[0])

	var condition: Callable = func() -> bool:
		var results: Array[Node] = SELECTOR_ENGINE.query(get_tree(), selector)
		if results.is_empty() or SELF_PROTECTION.is_protected(results[0]):
			return false
		return evaluate_property_condition(results[0], property, operator, expected_value)

	var success: bool = await wait_condition(condition, timeout_ms, poll_interval_ms)

	if success:
		return {"success": true, "found": true, "met_condition": true, "message": "Property condition satisfied within timeout period"}
	else:
		return ERRORS.make(
			ERRORS.TIMEOUT,
			"Property condition was not met before timeout (selector: %s, property: %s, operator: %s, timeout: %dms)"
				% [selector, property, operator, timeout_ms],
			{
				"selector": selector,
				"property": property,
				"operator": operator,
				"timeout_ms": timeout_ms,
				"next_action": "Raise timeout_ms, or read the property with get_property to see the value it actually holds.",
			}
		)


## Number of arguments a signal declares, or 0 if the node does not list it.
static func _signal_arg_count(node: Node, signal_name: String) -> int:
	for info: Dictionary in node.get_signal_list():
		if info.get("name") == signal_name:
			var declared: Array = info.get("args", [])
			return declared.size()
	return 0


## One-shot receiver whose arity matches the signal's declared argument count
## exactly. A fixed-arity lambda with defaulted parameters cannot tell a real
## null from a missing argument, so the count comes from the signal declaration
## and every argument, nulls included, is recorded. GDScript has no variadic
## lambdas, hence one arm per arity up to MAX_SIGNAL_ARGS.
static func _make_signal_capture(state: Array, argc: int) -> Callable:
	match argc:
		0:
			return func() -> void:
				state[0] = true
				state[1] = []
		1:
			return func(a1: Variant) -> void:
				state[0] = true
				state[1] = [a1]
		2:
			return func(a1: Variant, a2: Variant) -> void:
				state[0] = true
				state[1] = [a1, a2]
		3:
			return func(a1: Variant, a2: Variant, a3: Variant) -> void:
				state[0] = true
				state[1] = [a1, a2, a3]
		4:
			return func(a1: Variant, a2: Variant, a3: Variant, a4: Variant) -> void:
				state[0] = true
				state[1] = [a1, a2, a3, a4]
		5:
			return func(a1: Variant, a2: Variant, a3: Variant, a4: Variant, a5: Variant) -> void:
				state[0] = true
				state[1] = [a1, a2, a3, a4, a5]
		6:
			return func(a1: Variant, a2: Variant, a3: Variant, a4: Variant, a5: Variant, a6: Variant) -> void:
				state[0] = true
				state[1] = [a1, a2, a3, a4, a5, a6]
		7:
			return func(a1: Variant, a2: Variant, a3: Variant, a4: Variant, a5: Variant, a6: Variant, a7: Variant) -> void:
				state[0] = true
				state[1] = [a1, a2, a3, a4, a5, a6, a7]
		_:
			return func(a1: Variant, a2: Variant, a3: Variant, a4: Variant, a5: Variant, a6: Variant, a7: Variant, a8: Variant) -> void:
				state[0] = true
				state[1] = [a1, a2, a3, a4, a5, a6, a7, a8]


## Canonical failure for a target node that was freed mid-wait.
static func _node_freed(selector: String, phase: String, elapsed_ms: int) -> Dictionary:
	return ERRORS.make(ERRORS.NODE_NOT_FOUND, "Node freed %s" % phase, {
		"selector": selector,
		"phase": phase,
		"elapsed_ms": elapsed_ms,
		"next_action": "Wait on a node that outlives the operation, or re-query after the scene settles.",
	})


static func _to_float(v: Variant) -> float:
	if v is float:
		return v
	if v is int:
		var i: int = v
		return float(i)
	return 0.0


## JSON-decode a stringified `expected_value` when the actual property value's
## type cannot hold text. Mirrors property_handler.gd's _parse_stringified_json
## so a client that left expected_value untyped (godot-stagehand-wait-for-property-
## stringified-expected-60sz) and sent it as raw JSON text still compares
## correctly against a numeric or boolean property — but "equals"/"not_equals"
## against a String property must keep comparing the string literally.
## JSON.new().parse() is used over JSON.parse_string() because the latter
## pushes an engine error into the host game's log on every non-JSON string.
static func _parse_stringified_expected(expected_value: Variant, actual_value: Variant) -> Variant:
	if not (expected_value is String):
		return expected_value
	if actual_value == null or actual_value is String or actual_value is StringName:
		return expected_value
	var text: String = expected_value
	var json: JSON = JSON.new()
	if json.parse(text) != OK:
		return expected_value
	if json.data == null:
		return expected_value
	return json.data
