extends Node


const SELECTOR_ENGINE := preload("res://addons/stagehand/core/selector_engine.gd")


## Polling mechanism that waits for a condition to become true.
## Returns true if condition was met within timeout, false otherwise.
func wait_condition(condition_func: Callable, timeout_ms: int = 10000, poll_interval_ms: int = 100) -> bool:
	var start_time: int = Time.get_ticks_msec()
	var end_time: int = start_time + timeout_ms

	while Time.get_ticks_msec() < end_time:
		if condition_func.call():
			return true
		await get_tree().create_timer(poll_interval_ms * 0.001).timeout

	if condition_func.call():
		return true

	return false


## Evaluate different types of property conditions.
func evaluate_property_condition(node: Node, property_name: String, operator: String, expected_value: Variant) -> bool:
	if not node.has_meta(property_name) and not node.has_method("get_" + property_name) and not (property_name in node):
		return false

	var actual_value: Variant = null
	if node.has_method("get_" + property_name):
		actual_value = node.call("get_" + property_name)
	elif property_name in node:
		actual_value = node.get(property_name)
	elif node.has_meta(property_name):
		actual_value = node.get_meta(property_name)

	match operator:
		"equals":
			return actual_value == expected_value
		"not_equals":
			return actual_value != expected_value
		"exists":
			return actual_value != null
		"contains":
			if actual_value is String:
				if expected_value is String:
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
			var actual_f: float = _to_float(actual_value)
			var expected_f: float = _to_float(expected_value)
			if (actual_value is float or actual_value is int) and (expected_value is float or expected_value is int):
				return actual_f > expected_f
			return false
		"less_than":
			var actual_f: float = _to_float(actual_value)
			var expected_f: float = _to_float(expected_value)
			if (actual_value is float or actual_value is int) and (expected_value is float or expected_value is int):
				return actual_f < expected_f
			return false
		_:
			push_error("Unknown operator: " + operator)
			return false


## Wait for a node matching the selector to reach the desired state.
## state: "exists" (node in tree), "visible" (node in tree and visible), "removed" (node absent)
func wait_for_node(selector: String, state: String = "exists", timeout_ms: int = 10000, poll_interval_ms: int = 100) -> Dictionary:
	var condition: Callable = func() -> bool:
		var results: Array[Node] = SELECTOR_ENGINE.query(get_tree(), selector)
		match state:
			"exists":
				return results.size() > 0
			"visible":
				if results.size() > 0 and results[0] is CanvasItem:
					var ci: CanvasItem = results[0]
					return ci.visible
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
		match state:
			"removed":
				return {"success": false, "removed": false, "error": "Node did not disappear before timeout (selector: %s, timeout: %dms)" % [selector, timeout_ms]}
			"visible":
				return {"success": false, "found": false, "error": "Node did not become visible before timeout (selector: %s, timeout: %dms)" % [selector, timeout_ms]}
			_:
				return {"success": false, "found": false, "error": "Node did not appear before timeout (selector: %s, timeout: %dms)" % [selector, timeout_ms]}


## Wait for a signal to be emitted on a node.
## Uses one-shot signal connection with timer-based timeout (no polling).
func wait_for_signal(selector: String, signal_name: String, timeout_ms: int = 5000) -> Dictionary:
	var start_time: int = Time.get_ticks_msec()

	var results: Array[Node] = SELECTOR_ENGINE.query(get_tree(), selector)
	if results.is_empty():
		return {"received": false, "elapsed_ms": 0, "error": "Node not found: %s" % selector}

	var node: Node = results[0]

	if not node.has_signal(signal_name):
		return {"received": false, "elapsed_ms": 0, "error": "Signal '%s' not found on node '%s'" % [signal_name, node.get_path()]}

	# Use an Array to share state with the lambda (captures are by value for reassignment)
	var state: Array = [false, []]  # [received, signal_args]

	# Create a one-shot callback that captures signal arguments.
	var callback: Callable = func(arg1: Variant = null, arg2: Variant = null, arg3: Variant = null, arg4: Variant = null,
			arg5: Variant = null, arg6: Variant = null, arg7: Variant = null, arg8: Variant = null) -> void:
		state[0] = true
		var args: Array = []
		for arg: Variant in [arg1, arg2, arg3, arg4, arg5, arg6, arg7, arg8]:
			if arg == null:
				break
			args.append(arg)
		state[1] = args

	if not is_instance_valid(node):
		return {"received": false, "elapsed_ms": Time.get_ticks_msec() - start_time, "error": "Node freed before signal connection"}

	var _err: int = node.connect(signal_name, callback, CONNECT_ONE_SHOT)

	var end_time: int = start_time + timeout_ms
	while not state[0] and Time.get_ticks_msec() < end_time:
		if not is_instance_valid(node):
			return {"received": false, "elapsed_ms": Time.get_ticks_msec() - start_time, "error": "Node freed while waiting for signal"}
		await get_tree().create_timer(0.01).timeout

	var elapsed: int = Time.get_ticks_msec() - start_time

	if state[0]:
		return {"received": true, "elapsed_ms": elapsed, "args": state[1]}

	if is_instance_valid(node) and node.is_connected(signal_name, callback):
		node.disconnect(signal_name, callback)

	return {"received": false, "elapsed_ms": elapsed, "reason": "timeout"}


## Wait for a node's property to satisfy a condition.
func wait_for_property(selector: String, property: String, operator: String, expected_value: Variant, timeout_ms: int = 10000, poll_interval_ms: int = 100) -> Dictionary:
	var condition: Callable = func() -> bool:
		var results: Array[Node] = SELECTOR_ENGINE.query(get_tree(), selector)
		if results.is_empty():
			return false
		return evaluate_property_condition(results[0], property, operator, expected_value)

	var success: bool = await wait_condition(condition, timeout_ms, poll_interval_ms)

	if success:
		return {"success": true, "found": true, "met_condition": true, "message": "Property condition satisfied within timeout period"}
	else:
		return {"success": false, "met_condition": false, "error": "Property condition was not met before timeout (selector: %s, property: %s, operator: %s, timeout: %dms)" % [selector, property, operator, timeout_ms]}


static func _to_float(v: Variant) -> float:
	if v is float:
		return v
	if v is int:
		var i: int = v
		return float(i)
	return 0.0
