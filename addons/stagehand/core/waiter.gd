extends Node


const SELECTOR_ENGINE = preload("res://addons/stagehand/core/selector_engine.gd")


func wait_condition(condition_func, timeout_ms: int = 10000, poll_interval_ms: int = 100):
	"""
	Polling mechanism that waits for a condition to become true.
	
	Args:
	    condition_func: Callable returning true when condition is met
	    timeout_ms: Maximum time to wait in milliseconds
	    poll_interval_ms: Interval between polls in milliseconds
	
	Returns:
	    bool: True if condition was met within timeout, False otherwise
	"""
	var start_time := Time.get_ticks_msec()
	var end_time := start_time + timeout_ms
	
	while Time.get_ticks_msec() < end_time:
		if condition_func.call():
			return true
		await get_tree().create_timer(poll_interval_ms * 0.001).timeout
		
	# Try once more at very end in case of timing issues
	if condition_func.call():
		return true
	
	return false


func evaluate_property_condition(node, property_name: String, operator: String, expected_value) -> bool:
	"""
	Evaluate different types of property conditions.
	
	Args:
	    node: The target node to test
	    property_name: Name of the property to check
	    operator: Condition operator
	    expected_value: Expected value to compare against
	
	Returns:
	    bool: Result of the property comparison
	"""
	if not node.has_meta(property_name) and not node.has_method("get_" + property_name) and not node.property_exists(property_name):
		# Handle the case where property doesn't exist based on operator
		if operator == "exists":
			return false
		return false
	
	# Get the actual value of the property
	var actual_value = null
	if node.has_method("get_" + property_name):
		actual_value = node.call("get_" + property_name)
	elif node.property_exists(property_name):
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
			# Check if actual_value is something that can contain the expected value
			if actual_value is String and expected_value is String:
				return actual_value.contains(expected_value)
			elif actual_value is Array:
				return actual_value.has(expected_value)
			elif actual_value is Dictionary or actual_value is CallableBuiltIn:
				# For dictionaries, check if expected_value exists as key
				if typeof(actual_value) == TYPE_DICTIONARY:
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


func wait_for_node(selector: String, timeout_ms: int = 10000, poll_interval_ms: int = 100):
	"""
	Wait for a node matching the selector to appear in the scene tree.
	
	Args:
	    selector: Node selector expression
	    timeout_ms: Maximum time to wait in milliseconds
	    poll_interval_ms: Interval between polls in milliseconds
	
	Returns:
	    Dictionary: Result dictionary with 'found' boolean and additional info
	"""
	var condition = func() -> bool:
		var engine_instance = SELECTOR_ENGINE.new()
		var result = engine_instance.query_selector(selector)
		engine_instance.free()
		
		if result.has("error") or not result.has("nodes"):
			return false
		
		return result.nodes.size() > 0
	
	var success = wait_condition(condition, timeout_ms, poll_interval_ms)
	
	var final_result = {}
	if success:
		final_result["success"] = true
		final_result["found"] = true
		final_result["message"] = "Node found within timeout period"
	else:
		final_result["success"] = false
		final_result["found"] = false
		final_result["error"] = "Node did not appear before timeout (selector: %s, timeout: %dms)" % [selector, timeout_ms]
	
	return final_result


func wait_for_property(selector: String, property: String, operator: String, expected_value, timeout_ms: int = 10000, poll_interval_ms: int = 100):
	"""
	Wait for a node's property to satisfy a condition.
	
	Args:
	    selector: Node selector expression
	    property: Name of the property to monitor
	    operator: Operator to apply (equals, not_equals, exists, contains, greater_than, less_than)
	    expected_value: Value to compare against
	    timeout_ms: Maximum time to wait in milliseconds
	    poll_interval_ms: Interval between polls in milliseconds
	
	Returns:
	    Dictionary: Result dictionary with status information
	"""
	var condition = func() -> bool:
		var engine_instance = SELECTOR_ENGINE.new()
		var result = engine_instance.query_selector(selector)
		engine_instance.free()
		
		if result.has("error") or not result.has("nodes") or result.nodes.is_empty():
			return false
		
		var node = result.nodes[0]
		return evaluate_property_condition(node, property, operator, expected_value)
	
	var success = wait_condition(condition, timeout_ms, poll_interval_ms)
	
	var final_result = {}
	if success:
		final_result["success"] = true
		final_result["found"] = true
		final_result["met_condition"] = true
		final_result["message"] = "Property condition satisfied within timeout period"
	else:
		final_result["success"] = false
		final_result["met_condition"] = false
		final_result["error"] = "Property condition was not met before timeout (selector: %s, property: %s, operator: %s, timeout: %dms)" % [selector, property, operator, timeout_ms]
	
	return final_result