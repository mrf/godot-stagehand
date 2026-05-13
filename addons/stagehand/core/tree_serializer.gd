class_name StagehandTreeSerializer
extends RefCounted
## Serializes the scene tree and node properties into JSON-safe dictionaries.

const SelectorEngine := preload("res://addons/stagehand/core/selector_engine.gd")


## Serialize a snapshot of the scene tree starting at [param root_node].
## [param max_depth] limits recursion. Roots at the node do not count against depth.
## [param include_properties] is a list of property names to include per node.
static func serialize_tree(root_node: Node, max_depth: int = 10, include_properties: Array[String] = []) -> Dictionary:
	var root_data := _serialize_node(root_node, 0, max_depth, include_properties)
	root_data["count"] = _count_nodes(root_node)
	return root_data


## Find nodes matching [param selector] and return serialized results.
## [param properties] is a list of property names to include per match.
## [param limit] caps the number of results.
static func query_nodes(tree: SceneTree, selector: String, properties: Array[String] = [], limit: int = 50) -> Dictionary:
	var nodes: Array[Node] = SelectorEngine.query(tree, selector)
	var results: Array[Dictionary] = []
	var count := min(nodes.size(), limit)
	for i: int in range(count):
		results.append(_serialize_node_basic(nodes[i], properties))
	return {
		"nodes": results,
		"count": nodes.size(),
	}


static func _serialize_node(node: Node, depth: int, max_depth: int, include_properties: Array[String]) -> Dictionary:
	var data := _node_info(node)
	if include_properties.size() > 0:
		data["properties"] = _get_properties(node, include_properties)
	if depth < max_depth:
		var children: Array[Dictionary] = []
		for child: Node in node.get_children():
			children.append(_serialize_node(child, depth + 1, max_depth, include_properties))
		data["children"] = children
	return data


static func _serialize_node_basic(node: Node, properties: Array[String]) -> Dictionary:
	var data := _node_info(node)
	if properties.size() > 0:
		data["properties"] = _get_properties(node, properties)
	return data


static func _node_info(node: Node) -> Dictionary:
	return {
		"name": node.name,
		"class": node.get_class(),
		"path": node.get_path(),
	}


static func _get_properties(node: Node, names: Array[String]) -> Dictionary:
	var result := {}
	for prop: String in names:
		var value: Variant = _get_property_deep(node, prop)
		if value != null:
			result[prop] = _to_json_safe(value)
	return result


## Read a property from a node, supporting dot notation (e.g. "position.x").
static func _get_property_deep(node: Node, property: String) -> Variant:
	if node == null:
		return null
	var parts: PackedStringArray = property.split(".")
	var current: Variant = node
	for part: String in parts:
		if current is Object:
			current = current.get(part)
		elif current is Dictionary:
			current = current.get(part, null)
		elif current is Array and part.is_valid_int():
			var idx: int = part.to_int()
			if idx >= 0 and idx < current.size():
				current = current[idx]
			else:
				return null
		else:
			return null
	return current


## Convert common Godot types to JSON-safe primitives.
static func _to_json_safe(value: Variant) -> Variant:
	match typeof(value):
		TYPE_VECTOR2:
			var v: Vector2 = value
			return {"x": v.x, "y": v.y}
		TYPE_VECTOR2I:
			var v: Vector2i = value
			return {"x": v.x, "y": v.y}
		TYPE_VECTOR3:
			var v: Vector3 = value
			return {"x": v.x, "y": v.y, "z": v.z}
		TYPE_VECTOR3I:
			var v: Vector3i = value
			return {"x": v.x, "y": v.y, "z": v.z}
		TYPE_RECT2:
			var r: Rect2 = value
			return {"x": r.position.x, "y": r.position.y, "width": r.size.x, "height": r.size.y}
		TYPE_RECT2I:
			var r: Rect2i = value
			return {"x": r.position.x, "y": r.position.y, "width": r.size.x, "height": r.size.y}
		TYPE_COLOR:
			var c: Color = value
			return {"r": c.r, "g": c.g, "b": c.b, "a": c.a}
		TYPE_TRANSFORM2D:
			var t: Transform2D = value
			return {
				"x": _to_json_safe(t.x),
				"y": _to_json_safe(t.y),
				"origin": _to_json_safe(t.origin),
			}
		TYPE_TRANSFORM3D:
			var t: Transform3D = value
			return {
				"basis": _to_json_safe(t.basis),
				"origin": _to_json_safe(t.origin),
			}
		TYPE_BASIS:
			var b: Basis = value
			return {
				"x": _to_json_safe(b.x),
				"y": _to_json_safe(b.y),
				"z": _to_json_safe(b.z),
			}
		TYPE_STRING_NAME:
			return String(value)
		TYPE_NODE_PATH:
			return String(value)
		TYPE_RID:
			return "RID"
		TYPE_OBJECT:
			if value is Node:
				return _node_info(value)
			return str(value)
	return value


static func _count_nodes(root: Node) -> int:
	var count: int = 1
	for child: Node in root.get_children():
		count += _count_nodes(child)
	return count
