## Handles get_property and set_property commands.
class_name StagehandPropertyHandler
extends RefCounted

const ERRORS := preload("res://addons/stagehand/core/errors.gd")
const SELECTOR_ENGINE := preload("res://addons/stagehand/core/selector_engine.gd")
const TREE_SERIALIZER := preload("res://addons/stagehand/core/tree_serializer.gd")


## Get a property from a node matched by a selector.
## Supports dot notation (e.g. "position.x").
static func get_property(tree: SceneTree, params: Dictionary) -> Dictionary:
	var selector: String = params.get("selector", "")
	var property: String = params.get("property", "")

	if selector.is_empty():
		return ERRORS.missing_param("selector")
	if property.is_empty():
		return ERRORS.missing_param("property")

	var nodes: Array[Node] = SELECTOR_ENGINE.query(tree, selector)
	if nodes.is_empty():
		return ERRORS.node_not_found(selector)

	var node: Node = nodes[0]
	var value: Variant = TREE_SERIALIZER._get_property_deep(node, property)
	if value == null and not _has_property(node, property):
		return _property_not_found(selector, node, property)

	return {
		"value": TREE_SERIALIZER._to_json_safe(value),
		"type": type_string(typeof(value)),
	}


## Set a property on a node matched by a selector.
## `success` reflects a post-set read-back against the requested value, not
## merely that the property was found — a custom setter, a read-only
## property, or a type mismatch can make `Object.set()` a silent no-op, and a
## found-only check would misreport that as success (godot-stagehand-jzs).
static func set_property(tree: SceneTree, params: Dictionary) -> Dictionary:
	var selector: String = params.get("selector", "")
	var property: String = params.get("property", "")

	if selector.is_empty():
		return ERRORS.missing_param("selector")
	if property.is_empty():
		return ERRORS.missing_param("property")

	var nodes: Array[Node] = SELECTOR_ENGINE.query(tree, selector)
	if nodes.is_empty():
		return ERRORS.node_not_found(selector)

	var node: Node = nodes[0]
	var previous: Variant = _get_property_at_level(node, property)
	var requested_value: Variant = params.get("value")
	var target_type: int = _declared_property_type(node, property, previous)
	var converted: Dictionary = _coerce_json_value(requested_value, target_type)
	if not _conversion_succeeded(converted):
		var conversion_error: String = converted.get(
			"error", "Invalid value for property: %s" % property
		)
		return ERRORS.make(ERRORS.INVALID_VALUE, conversion_error, {
			"selector": selector,
			"node_path": str(node.get_path()),
			"property": property,
			"requested_value": TREE_SERIALIZER._to_json_safe(requested_value),
			"target_type": type_string(target_type),
			"next_action": "Send a value the property's declared type accepts; read it back with get_property to see the current type.",
		})

	var converted_value: Variant = converted.get("value")
	var found: bool = _set_property_deep(node, property, converted_value)
	if not found:
		return _property_not_found(selector, node, property)

	var applied_value: Variant = _get_property_at_level(node, property)
	var success: bool = is_same(applied_value, converted_value)
	if not success and not _has_property(node, property):
		return _property_not_found(selector, node, property)

	return {
		"success": success,
		"previous_value": TREE_SERIALIZER._to_json_safe(previous),
	}


## The canonical failure for a property that the matched node does not expose.
## Shared by get_property and set_property so both report the same kind and the
## same context for the same underlying condition.
static func _property_not_found(selector: String, node: Node, property: String) -> Dictionary:
	return ERRORS.make(ERRORS.PROPERTY_NOT_FOUND, "Property not found: %s" % property, {
		"selector": selector,
		"node_path": str(node.get_path()),
		"property": property,
		"node_class": node.get_class(),
		"next_action": "Call get_tree with the properties argument, or query_nodes, to list the properties this node exposes.",
	})


## Convert JSON-safe MCP values to the existing property's Godot value type.
## JSON has no Vector or Color types, and some MCP clients serialize an
## unconstrained boolean as a String. Conversion is target-aware so a String
## property whose value is literally "false" remains a String.
static func _declared_property_type(node: Node, property: String, current: Variant) -> int:
	if property.find(".") >= 0:
		return typeof(current)
	for info: Dictionary in node.get_property_list():
		if info.get("name", "") == property:
			var declared_type: Variant = info.get("type", typeof(current))
			if declared_type is int:
				var type_id: int = declared_type
				return type_id
	return typeof(current)


static func _coerce_json_value(value: Variant, target_type: int) -> Dictionary:
	if value == null or typeof(value) == target_type:
		return {"success": true, "value": value}

	# A client whose schema left `value` untyped sends the argument as raw JSON
	# text, so "50" and '{"x": 1.5, "y": 2}' arrive as Strings rather than as a
	# number and a Dictionary (godot-stagehand-set-property-value-stringified-e7er).
	# Parsing is target-aware and never applies to a String or Variant target,
	# so a String property asked to hold "50" still holds the two characters.
	var parsed: Variant = _parse_stringified_json(value, target_type)

	match target_type:
		TYPE_BOOL:
			if parsed is bool:
				var parsed_bool: bool = parsed
				return {"success": true, "value": parsed_bool}
			# Belt-and-braces for text that JSON cannot parse as a bool at all
			# ("True", " false "); this predates the stringified-value fix.
			if value is String:
				var bool_text: String = value
				match bool_text.strip_edges().to_lower():
					"true":
						return {"success": true, "value": true}
					"false":
						return {"success": true, "value": false}
			return _conversion_failure(target_type)
		TYPE_FLOAT:
			return _coerce_float(parsed)
		TYPE_INT:
			return _coerce_int(parsed)
		TYPE_VECTOR2:
			return _coerce_vector2(parsed)
		TYPE_VECTOR2I:
			return _coerce_vector2i(parsed)
		TYPE_VECTOR3:
			return _coerce_vector3(parsed)
		TYPE_COLOR:
			return _coerce_color(parsed)

	return {"success": true, "value": value}


## JSON-decode a stringified `value` when the target type cannot be a String.
## Returns the value untouched when it is not a String, when the target would
## legitimately hold text, or when the text is not valid JSON — a numeric target
## given "not a number" must still fail its conversion rather than land as 0.
## JSON.new().parse() is used over JSON.parse_string() because the latter pushes
## an engine error on every non-JSON string, which would spam the host game's
## log for what is an ordinary rejected set.
static func _parse_stringified_json(value: Variant, target_type: int) -> Variant:
	if not (value is String):
		return value
	if target_type == TYPE_NIL or target_type == TYPE_STRING or target_type == TYPE_STRING_NAME:
		return value
	var text: String = value
	var json: JSON = JSON.new()
	if json.parse(text) != OK:
		return value
	if json.data == null:
		return value
	return json.data


static func _coerce_vector2(value: Variant) -> Dictionary:
	var x: Dictionary = _float_component(value, "x", 0)
	var y: Dictionary = _float_component(value, "y", 1)
	if not _conversion_succeeded(x) or not _conversion_succeeded(y):
		return _conversion_failure(TYPE_VECTOR2)
	return {"success": true, "value": Vector2(_result_float(x), _result_float(y))}


static func _coerce_vector2i(value: Variant) -> Dictionary:
	var x: Dictionary = _int_component(value, "x", 0)
	var y: Dictionary = _int_component(value, "y", 1)
	if not _conversion_succeeded(x) or not _conversion_succeeded(y):
		return _conversion_failure(TYPE_VECTOR2I)
	return {"success": true, "value": Vector2i(_result_int(x), _result_int(y))}


static func _coerce_vector3(value: Variant) -> Dictionary:
	var x: Dictionary = _float_component(value, "x", 0)
	var y: Dictionary = _float_component(value, "y", 1)
	var z: Dictionary = _float_component(value, "z", 2)
	if (
		not _conversion_succeeded(x)
		or not _conversion_succeeded(y)
		or not _conversion_succeeded(z)
	):
		return _conversion_failure(TYPE_VECTOR3)
	return {
		"success": true,
		"value": Vector3(_result_float(x), _result_float(y), _result_float(z)),
	}


static func _coerce_color(value: Variant) -> Dictionary:
	var red: Dictionary = _float_component(value, "r", 0)
	var green: Dictionary = _float_component(value, "g", 1)
	var blue: Dictionary = _float_component(value, "b", 2)
	var alpha: Dictionary = _float_component(value, "a", 3)
	if (
		not _conversion_succeeded(red)
		or not _conversion_succeeded(green)
		or not _conversion_succeeded(blue)
		or not _conversion_succeeded(alpha)
	):
		return _conversion_failure(TYPE_COLOR)
	return {
		"success": true,
		"value": Color(
			_result_float(red),
			_result_float(green),
			_result_float(blue),
			_result_float(alpha)
		),
	}


static func _float_component(value: Variant, key: String, index: int) -> Dictionary:
	var component: Dictionary = _json_component(value, key, index)
	if not _conversion_succeeded(component):
		return component
	return _coerce_float(component.get("value"))


static func _int_component(value: Variant, key: String, index: int) -> Dictionary:
	var component: Dictionary = _json_component(value, key, index)
	if not _conversion_succeeded(component):
		return component
	return _coerce_int(component.get("value"))


static func _json_component(value: Variant, key: String, index: int) -> Dictionary:
	if value is Dictionary:
		var dictionary: Dictionary = value
		if dictionary.has(key):
			return {"success": true, "value": dictionary.get(key)}
	elif value is Array:
		var array: Array = value
		if index >= 0 and index < array.size():
			return {"success": true, "value": array[index]}
	return {"success": false}


static func _coerce_float(value: Variant) -> Dictionary:
	if value is float:
		var float_value: float = value
		return {"success": true, "value": float_value}
	if value is int:
		var int_value: int = value
		return {"success": true, "value": float(int_value)}
	return {"success": false}


static func _coerce_int(value: Variant) -> Dictionary:
	if value is int:
		var int_value: int = value
		return {"success": true, "value": int_value}
	if value is float:
		var float_value: float = value
		var converted: int = int(float_value)
		if is_equal_approx(float_value, float(converted)):
			return {"success": true, "value": converted}
	return {"success": false}


static func _conversion_succeeded(result: Dictionary) -> bool:
	var success_value: Variant = result.get("success", false)
	if success_value is bool:
		var success: bool = success_value
		return success
	return false


static func _result_float(result: Dictionary) -> float:
	var value: Variant = result.get("value", 0.0)
	if value is float:
		var float_value: float = value
		return float_value
	return 0.0


static func _result_int(result: Dictionary) -> int:
	var value: Variant = result.get("value", 0)
	if value is int:
		var int_value: int = value
		return int_value
	return 0


static func _conversion_failure(target_type: int) -> Dictionary:
	return {
		"success": false,
		"error": "Value cannot be converted to %s" % type_string(target_type),
	}


## Check if a node has a property (supports dot notation).
static func _has_property(node: Node, property: String) -> bool:
	var parts: PackedStringArray = property.split(".")
	var current: Variant = node
	for i: int in range(parts.size()):
		var part: String = parts[i]
		if current is Object:
			var obj: Object = current
			if not obj.get_property_list().any(
				func(info: Dictionary) -> bool: return info.get("name", "") == part
			):
				# For built-in properties that may not show up in get_property_list,
				# fall through to get() if we haven't reached the last part.
				var _tmp: Variant = obj.get(part)
				if _tmp == null:
					return false
				current = _tmp
		else:
			return false
	return true


## Get the property value at the top level (for previous_value capture).
static func _get_property_at_level(node: Node, property: String) -> Variant:
	var dot_idx: int = property.find(".")
	if dot_idx >= 0:
		var top: String = property.substr(0, dot_idx)
		var obj: Variant = node.get(top) if node != null else null
		if obj == null:
			return null
		return TREE_SERIALIZER._get_property_deep(node, property)
	return node.get(property) if node != null else null


## Set a property on a node, supporting dot notation for nested properties.
static func _set_property_deep(node: Node, property: String, value: Variant) -> bool:
	var parts: PackedStringArray = property.split(".")
	if parts.size() == 1:
		return _do_set(node, property, value)

	# Walking Object.get() part-by-part cannot handle a chain that descends
	# into a built-in struct property ("position.x"): get("position") returns a
	# Vector2, which is not an Object, so the walk both fails and — because the
	# cursor was typed as Object — raised "Trying to assign value of type
	# 'Vector2' to a variable of type 'Object'". set_indexed walks the
	# ':'-joined path natively, handling struct components and nested Objects
	# alike, and silently no-ops on an invalid path — so confirm by read-back.
	var indexed_path: NodePath = NodePath(":".join(parts))
	if node.get_indexed(indexed_path) == null:
		return false
	node.set_indexed(indexed_path, value)
	return true


## Attempt to set a property on an object.
static func _do_set(obj: Object, property: String, value: Variant) -> bool:
	if obj == null:
		return false
	var has_property: bool = obj.get_property_list().any(
		func(info: Dictionary) -> bool: return info.get("name", "") == property
	)
	if has_property:
		obj.set(property, value)
		return true
	# For indexed built-ins (for example position:x), try set_indexed as a best effort.
	if obj.has_method("set_indexed"):
		obj.set_indexed(NodePath(property), value)
		return true
	return false
