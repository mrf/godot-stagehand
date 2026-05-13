## Handles get_property and set_property commands.
class_name StagehandPropertyHandler
extends RefCounted

const SelectorEngine := preload("res://addons/stagehand/core/selector_engine.gd")
const StagehandTreeSerializer := preload("res://addons/stagehand/core/tree_serializer.gd")


## Get a property from a node matched by a selector.
## Supports dot notation (e.g. "position.x").
static func get_property(tree: SceneTree, params: Dictionary) -> Dictionary:
	var selector: String = params.get("selector", "")
	var property: String = params.get("property", "")

	if selector.is_empty() or property.is_empty():
		return {"error": "Missing selector or property"}

	var nodes: Array[Node] = SelectorEngine.query(tree, selector)
	if nodes.is_empty():
		return {"error": "Node not found for selector: %s" % selector}

	var node: Node = nodes[0]
	var value: Variant = StagehandTreeSerializer._get_property_deep(node, property)
	if value == null and not _has_property(node, property):
		return {"error": "Property not found: %s" % property}

	return {
		"value": StagehandTreeSerializer._to_json_safe(value),
		"type": type_string(typeof(value)),
	}


## Set a property on a node matched by a selector.
static func set_property(tree: SceneTree, params: Dictionary) -> Dictionary:
	var selector: String = params.get("selector", "")
	var property: String = params.get("property", "")

	if selector.is_empty() or property.is_empty():
		return {"error": "Missing selector or property"}

	var nodes: Array[Node] = SelectorEngine.query(tree, selector)
	if nodes.is_empty():
		return {"error": "Node not found for selector: %s" % selector}

	var node: Node = nodes[0]
	var previous: Variant = _get_property_at_level(node, property)
	var success := _set_property_deep(node, property, params.get("value"))
	if not success:
		return {"error": "Failed to set property: %s" % property}

	return {
		"success": true,
		"previous_value": StagehandTreeSerializer._to_json_safe(previous),
	}


## Check if a node has a property (supports dot notation).
static func _has_property(node: Node, property: String) -> bool:
	var parts: PackedStringArray = property.split(".")
	var current: Variant = node
	for i: int in range(parts.size()):
		var part: String = parts[i]
		if current is Object:
			if not current.get_property_list().any(
				func(info: Dictionary) -> bool: return info.get("name", "") == part
			):
				# For built-in properties that may not show up in get_property_list,
				# fall through to get() if we haven't reached the last part.
				var _tmp: Variant = current.get(part)
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
		var rest: String = property.substr(dot_idx + 1)
		var obj: Variant = node.get(top) if node != null else null
		if obj == null:
			return null
		return StagehandTreeSerializer._get_property_deep(node, property)
	return node.get(property) if node != null else null


## Set a property on a node, supporting dot notation for nested properties.
static func _set_property_deep(node: Node, property: String, value: Variant) -> bool:
	var parts: PackedStringArray = property.split(".")
	if parts.size() == 1:
		return _do_set(node, property, value)

	# For dot notation, resolve all but the last part to find the target object.
	var current: Object = node
	for i: int in range(parts.size() - 1):
		var part: String = parts[i]
		if current != null and current.has_method("get"):
			current = current.get(part)
		else:
			return false

	return _do_set(current, parts[parts.size() - 1], value)


static func _do_set(obj: Object, property: String, value: Variant) -> bool:
	if obj == null:
		return false
	var has_property := obj.get_property_list().any(
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
