## Parses selector strings and resolves them to arrays of matching nodes.
##
## Selector grammar (MVP):
##   "/root/UI/Button"    — exact node path via get_node()
##   "name:*Button*"      — recursive find_children() with glob matching
##   "class:Button"       — tree walk + is_class()
##   "group:interactive"  — get_nodes_in_group()
##
## Phase 2 adds text:, meta:, unique:, and >> chaining.
class_name SelectorEngine
extends RefCounted

enum SelectorType {
	PATH,
	NAME,
	CLASS,
	GROUP,
}


static func query(tree: SceneTree, selector: String) -> Array[Node]:
	var parsed := parse(selector)
	if parsed.is_empty():
		return [] as Array[Node]
	return _resolve(tree, parsed)


static func parse(selector: String) -> Dictionary:
	var trimmed := selector.strip_edges()
	if trimmed.is_empty():
		return {}

	if trimmed.begins_with("name:"):
		var pattern := trimmed.substr(5)
		if pattern.is_empty():
			return {}
		return {type = SelectorType.NAME, value = pattern}

	if trimmed.begins_with("class:"):
		var class_name_ := trimmed.substr(6)
		if class_name_.is_empty():
			return {}
		return {type = SelectorType.CLASS, value = class_name_}

	if trimmed.begins_with("group:"):
		var group_name := trimmed.substr(6)
		if group_name.is_empty():
			return {}
		return {type = SelectorType.GROUP, value = group_name}

	# No recognized prefix — treat as exact node path.
	return {type = SelectorType.PATH, value = trimmed}


static func _resolve(tree: SceneTree, parsed: Dictionary) -> Array[Node]:
	var type: SelectorType = parsed.type
	var value: String = parsed.value

	match type:
		SelectorType.PATH:
			return _resolve_path(tree, value)
		SelectorType.NAME:
			return _resolve_name(tree, value)
		SelectorType.CLASS:
			return _resolve_class(tree, value)
		SelectorType.GROUP:
			return _resolve_group(tree, value)

	return [] as Array[Node]


static func _resolve_path(tree: SceneTree, path: String) -> Array[Node]:
	var root := tree.root
	if root == null:
		return [] as Array[Node]
	var node := root.get_node_or_null(NodePath(path))
	if node == null:
		return [] as Array[Node]
	return [node] as Array[Node]


static func _resolve_name(tree: SceneTree, pattern: String) -> Array[Node]:
	var root := tree.root
	if root == null:
		return [] as Array[Node]
	# find_children handles both glob patterns (*, ?) and exact names.
	var results: Array[Node] = []
	results.assign(root.find_children(pattern))
	return results


static func _resolve_class(tree: SceneTree, class_name_: String) -> Array[Node]:
	var root := tree.root
	if root == null:
		return [] as Array[Node]
	var results: Array[Node] = []
	_walk(root, func(node: Node) -> void:
		if node.is_class(class_name_):
			results.append(node)
	)
	return results


static func _resolve_group(tree: SceneTree, group_name: String) -> Array[Node]:
	var results: Array[Node] = []
	results.assign(tree.get_nodes_in_group(group_name))
	return results


## Depth-first walk of the scene tree, calling visitor on every node.
static func _walk(root: Node, visitor: Callable) -> void:
	visitor.call(root)
	for child: Node in root.get_children():
		_walk(child, visitor)
