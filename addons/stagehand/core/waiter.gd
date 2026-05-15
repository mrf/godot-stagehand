extends Node


const SELECTOR_ENGINE = preload("res://addons/stagehand/core/selector_engine.gd")


## Polling mechanism that waits for a condition to become true.
## Returns true if condition was met within timeout, false otherwise.
func wait_condition(condition_func: Callable, timeout_ms: int = 10000, poll_interval_ms: int = 100) -> bool:
	var start_time := Time.get_ticks_msec()
	var end_time := start_time + timeout_ms

	while Time.get_ticks_msec() < end_time:
		if condition_func.call():
			return true
		await get_tree().create_timer(poll_interval_ms * 0.001).timeout

	if condition_func.call():
		return true

	return false


## Evaluate different types of property conditions.
func evaluate_property_condition(node: Node, property_name: String, operator: String, expected_value) -> bool:
	if not node.has_meta(property_name) and not node.has_method("get_" + property_name) and not (property_name in node):
		return false

	var actual_value = null
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
			if actual_value is String and expected_value is String:
				return actual_value.contains(expected_value)
			elif actual_value is Array:
				return actual_value.has(expected_value)
			elif actual_value is Dictionary:
				return actual_value.has(expected_value)
			return false
		"greater_than":
			if actual_value is float or actual_value is int:
				if expected_value is float or expected_value is int:
					return float(actual_value) > float(expected_value)
			return false
		"less_than":
			if actual_value is float or actual_value is int:
				if expected_value is float or expected_value is int:
					return float(actual_value) < float(expected_value)
			return false
		_:
			push_error("Unknown operator: " + operator)
			return false


## Wait for a node matching the selector to reach the desired state.
## state: "exists" (node in tree), "visible" (node in tree and visible), "removed" (node absent)
func wait_for_node(selector: String, state: String = "exists", timeout_ms: int = 10000, poll_interval_ms: int = 100) -> Dictionary:
	var condition := func() -> bool:
		var results: Array[Node] = SELECTOR_ENGINE.query(get_tree(), selector)
		match state:
			"exists":
				return results.size() > 0
			"visible":
				return results.size() > 0 and results[0].visible
			"removed":
				return results.size() == 0
			_:
				return results.size() > 0

	var success := await wait_condition(condition, timeout_ms, poll_interval_ms)

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


## Wait for a node's property to satisfy a condition.
func wait_for_property(selector: String, property: String, operator: String, expected_value, timeout_ms: int = 10000, poll_interval_ms: int = 100) -> Dictionary:
	var condition := func() -> bool:
		var results: Array[Node] = SELECTOR_ENGINE.query(get_tree(), selector)
		if results.is_empty():
			return false
		return evaluate_property_condition(results[0], property, operator, expected_value)

	var success := await wait_condition(condition, timeout_ms, poll_interval_ms)

	if success:
		return {"success": true, "found": true, "met_condition": true, "message": "Property condition satisfied within timeout period"}
	else:
		return {"success": false, "met_condition": false, "error": "Property condition was not met before timeout (selector: %s, property: %s, operator: %s, timeout: %dms)" % [selector, property, operator, timeout_ms]}
