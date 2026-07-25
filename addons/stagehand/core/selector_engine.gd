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
	TEXT,
	META,
	UNIQUE,
	TEXT_EXACT,
}


static func query(tree: SceneTree, selector: String) -> Array[Node]:
	if selector.contains(">>"):
		return query_chained_selector(tree, selector)
	var parsed: Dictionary = parse(selector)
	if parsed.is_empty():
		return [] as Array[Node]
	return _resolve(tree, parsed)


static func query_chained_selector(tree: SceneTree, selector_str: String) -> Array[Node]:
	var parts: PackedStringArray = selector_str.split(">>", false)

	var parsed_parts: Array[Dictionary] = []
	for part: String in parts:
		var trimmed_part: String = part.strip_edges()
		if trimmed_part.is_empty():
			push_error("Invalid empty selector part in chain: %s" % selector_str)
			return [] as Array[Node]
		var parsed: Dictionary = parse(trimmed_part)
		if parsed.is_empty():
			push_error("Failed to parse selector part: %s" % trimmed_part)
			return [] as Array[Node]
		parsed_parts.append(parsed)

	var current_matches: Array[Node] = _resolve(tree, parsed_parts[0])

	for i: int in range(1, parsed_parts.size()):
		var next_result: Array[Node] = []
		var current_selector: Dictionary = parsed_parts[i]
		for node: Node in current_matches:
			next_result.append_array(_resolve_scoped(node, current_selector))
		current_matches = next_result

	return current_matches


static func parse(selector: String) -> Dictionary:
	var trimmed: String = selector.strip_edges()
	if trimmed.is_empty():
		return {}

	if trimmed.begins_with("name:"):
		var pattern: String = trimmed.substr(5)
		if pattern.is_empty():
			return {}
		return {type = SelectorType.NAME, value = pattern}

	if trimmed.begins_with("class:"):
		var class_name_: String = trimmed.substr(6)
		if class_name_.is_empty():
			return {}
		return {type = SelectorType.CLASS, value = class_name_}

	if trimmed.begins_with("group:"):
		var group_name: String = trimmed.substr(6)
		if group_name.is_empty():
			return {}
		return {type = SelectorType.GROUP, value = group_name}

	if trimmed.begins_with("text="):
		var exact_text: String = trimmed.substr(5)
		if exact_text.is_empty():
			return {}
		return {type = SelectorType.TEXT_EXACT, value = exact_text}

	if trimmed.begins_with("text:"):
		var text_content: String = trimmed.substr(5)
		if text_content.is_empty():
			return {}
		return {type = SelectorType.TEXT, value = text_content}

	if trimmed.begins_with("meta:"):
		var meta_data: String = trimmed.substr(5)
		if meta_data.is_empty():
			return {}
		return {type = SelectorType.META, value = meta_data}

	if trimmed.begins_with("unique:"):
		var unique_id: String = trimmed.substr(7)
		if unique_id.is_empty():
			return {}
		return {type = SelectorType.UNIQUE, value = unique_id}

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
		SelectorType.TEXT:
			return _resolve_text(tree, value)
		SelectorType.TEXT_EXACT:
			return _resolve_text(tree, value, true)
		SelectorType.META:
			return _resolve_meta(tree, value)
		SelectorType.UNIQUE:
			return _resolve_unique(tree, value)

	return [] as Array[Node]


static func _resolve_scoped(parent_node: Node, parsed: Dictionary) -> Array[Node]:
	var type: SelectorType = parsed.type
	var value: String = parsed.value

	match type:
		SelectorType.PATH:
			return _resolve_path_from_parent(parent_node, value)
		SelectorType.NAME:
			return _resolve_name_from_parent(parent_node, value)
		SelectorType.CLASS:
			return _resolve_class_from_parent(parent_node, value)
		SelectorType.GROUP:
			return _resolve_group_from_parent(parent_node, value)
		SelectorType.TEXT:
			return _resolve_text_from_parent(parent_node, value)
		SelectorType.TEXT_EXACT:
			return _resolve_text_from_parent(parent_node, value, true)
		SelectorType.META:
			return _resolve_meta_from_parent(parent_node, value)
		SelectorType.UNIQUE:
			return _resolve_unique_from_parent(parent_node, value)

	return [] as Array[Node]


static func _resolve_path(tree: SceneTree, path: String) -> Array[Node]:
	var root: Window = tree.root
	if root == null:
		return [] as Array[Node]
	var node: Node = root.get_node_or_null(NodePath(path))
	if node == null:
		return [] as Array[Node]
	return [node] as Array[Node]


static func _resolve_name(tree: SceneTree, pattern: String) -> Array[Node]:
	var root: Window = tree.root
	if root == null:
		return [] as Array[Node]
	# find_children handles both glob patterns (*, ?) and exact names.
	# owned=false is required: it defaults to true, which restricts the search
	# to nodes owned by the scene root — i.e. nodes saved in a .tscn. Every
	# node instantiated at runtime (spawned enemies, bullets, dynamically built
	# UI) has a null owner and would be silently invisible to `name:`, while
	# every other selector type walks the tree and does find them.
	var results: Array[Node] = []
	results.assign(root.find_children(pattern, "", true, false))
	return results


static func _resolve_class(tree: SceneTree, class_name_: String) -> Array[Node]:
	var root: Window = tree.root
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


static func _resolve_text(tree: SceneTree, text_pattern: String, exact: bool = false) -> Array[Node]:
	var root: Window = tree.root
	if root == null:
		return [] as Array[Node]
	var results: Array[Node] = []
	_walk(root, func(node: Node) -> void:
		var node_text: String = _get_node_text(node)
		if node_text != "" and _matches_text(node_text, text_pattern, exact):
			results.append(node)
	)
	return results


static func _resolve_meta(tree: SceneTree, meta_expr: String) -> Array[Node]:
	var root: Window = tree.root
	if root == null:
		return [] as Array[Node]
	var results: Array[Node] = []
	var parsed: Array = _parse_meta_expr(meta_expr)
	var meta_key: String = parsed[0]
	var expected_value: String = parsed[1]

	_walk(root, func(node: Node) -> void:
		if _node_matches_meta(node, meta_key, expected_value):
			results.append(node)
	)
	return results


static func _resolve_unique(tree: SceneTree, unique_identifier: String) -> Array[Node]:
	var root: Window = tree.root
	if root == null:
		return [] as Array[Node]
	var results: Array[Node] = []
	_walk(root, func(node: Node) -> void:
		if _check_unique_match(node, unique_identifier):
			results.append(node)
	)
	return results


static func _resolve_path_from_parent(parent: Node, path: String) -> Array[Node]:
	if path.begins_with("/"):
		return [] as Array[Node]
	var node: Node = parent.get_node_or_null(NodePath(path))
	if node == null:
		return [] as Array[Node]
	return [node] as Array[Node]


static func _resolve_name_from_parent(parent: Node, pattern: String) -> Array[Node]:
	# owned=false for the same reason as _resolve_name: runtime-instantiated
	# descendants have a null owner and are excluded by the default.
	var results: Array[Node] = []
	results.assign(parent.find_children(pattern, "", true, false))
	return results


static func _resolve_class_from_parent(parent: Node, class_name_: String) -> Array[Node]:
	var results: Array[Node] = []
	_walk(parent, func(node: Node) -> void:
		if node != parent and node.is_class(class_name_):
			results.append(node)
	)
	return results


static func _resolve_group_from_parent(parent: Node, group_name: String) -> Array[Node]:
	var all_nodes_in_group: Array[Node] = parent.get_tree().get_nodes_in_group(group_name)
	var results: Array[Node] = []
	for node: Node in all_nodes_in_group:
		if node != parent and _is_ancestor_of(node, parent):
			results.append(node)
	return results


static func _resolve_text_from_parent(parent: Node, text_pattern: String, exact: bool = false) -> Array[Node]:
	var results: Array[Node] = []
	_walk(parent, func(node: Node) -> void:
		if node != parent:
			var node_text: String = _get_node_text(node)
			if node_text != "" and _matches_text(node_text, text_pattern, exact):
				results.append(node)
	)
	return results


static func _resolve_meta_from_parent(parent: Node, meta_expr: String) -> Array[Node]:
	var results: Array[Node] = []
	var parsed: Array = _parse_meta_expr(meta_expr)
	var meta_key: String = parsed[0]
	var expected_value: String = parsed[1]

	_walk(parent, func(node: Node) -> void:
		if node != parent and _node_matches_meta(node, meta_key, expected_value):
			results.append(node)
	)
	return results


static func _resolve_unique_from_parent(parent: Node, unique_identifier: String) -> Array[Node]:
	var results: Array[Node] = []
	_walk(parent, func(node: Node) -> void:
		if node != parent and _check_unique_match(node, unique_identifier):
			results.append(node)
	)
	return results


static func _get_node_text(node: Node) -> String:
	if node.has_method("get_text"):
		var val: Variant = node.call("get_text")
		if val is String:
			return val
	elif node.has_method("get_label"):
		var val: Variant = node.call("get_label")
		if val is String:
			return val
	elif node.has_method("get_title"):
		var val: Variant = node.call("get_title")
		if val is String:
			return val
	elif "text" in node:
		var val: Variant = node.get("text")
		if val is String:
			return val
	elif "label" in node:
		var val: Variant = node.get("label")
		if val is String:
			return val
	elif "title" in node:
		var val: Variant = node.get("title")
		if val is String:
			return val
	return ""


static func _check_unique_match(node: Node, unique_identifier: String) -> bool:
	# Match against node name first
	if _matches_pattern(node.name, unique_identifier):
		return true
	# Check other likely unique identifiers via get() to avoid type errors
	if "hint_tooltip" in node:
		var val: Variant = node.get("hint_tooltip")
		if val is String and str(val).match(unique_identifier):
			return true
	if "placeholder_text" in node:
		var val: Variant = node.get("placeholder_text")
		if val is String and str(val).match(unique_identifier):
			return true
	if "accessible_role" in node:
		var val: Variant = node.get("accessible_role")
		if str(val).to_lower().match(unique_identifier.to_lower()):
			return true
	return false


static func _is_ancestor_of(descendant: Node, ancestor: Node) -> bool:
	var current: Node = descendant.get_parent()
	while current:
		if current == ancestor:
			return true
		current = current.get_parent()
	return false


static func _matches_pattern(text: String, pattern: String) -> bool:
	if text.is_empty() or pattern.is_empty():
		return false

	if pattern.contains("*") or pattern.contains("?") or pattern.contains("["):
		return text.match(pattern)
	return text.to_lower().contains(pattern.to_lower())


## Match node text against a text selector value.
## When [param exact] is true, the node text (whitespace-trimmed) must equal the
## pattern exactly, case-sensitively (the `text=` form). Otherwise the loose
## `text:` semantics apply: glob match when the pattern contains *, ?, or [,
## else a case-insensitive substring match.
static func _matches_text(text: String, pattern: String, exact: bool) -> bool:
	if exact:
		return text.strip_edges() == pattern
	return _matches_pattern(text, pattern)


## Rank nodes so the most plausible interaction target comes first, for
## disambiguating selectors that resolve to more than one node (e.g. a
## descriptive Label and an actual Button both containing the same word).
##
## Tiers, highest priority first; order within a tier is preserved (stable):
##   1. BaseButton and its subclasses (Button, CheckBox, OptionButton, ...).
##   2. Other Controls that receive mouse input (mouse_filter != IGNORE).
##      Labels default to MOUSE_FILTER_IGNORE and therefore sort below buttons.
##   3. Everything else (ignore-filter Controls, Node2D, plain Nodes).
static func rank_for_interaction(nodes: Array[Node]) -> Array[Node]:
	var buttons: Array[Node] = []
	var interactive: Array[Node] = []
	var rest: Array[Node] = []
	for node: Node in nodes:
		if node is BaseButton:
			buttons.append(node)
		elif node is Control and (node as Control).mouse_filter != Control.MOUSE_FILTER_IGNORE:
			interactive.append(node)
		else:
			rest.append(node)
	var ranked: Array[Node] = []
	ranked.append_array(buttons)
	ranked.append_array(interactive)
	ranked.append_array(rest)
	return ranked


## Parse a meta expression "key=value" or "key" into [key, expected_value].
static func _parse_meta_expr(meta_expr: String) -> Array:
	var meta_key: String = meta_expr
	var expected_value: String = ""
	if meta_expr.contains("="):
		var eq_parts: PackedStringArray = meta_expr.split("=", false, 1)
		meta_key = eq_parts[0]
		expected_value = eq_parts[1] if eq_parts.size() > 1 else ""
	return [meta_key, expected_value]


## Check if a node matches a meta key/value pair.
static func _node_matches_meta(node: Node, meta_key: String, expected_value: String) -> bool:
	if meta_key in node:
		var actual_value: Variant = node[meta_key]
		if expected_value.is_empty():
			return true
		return str(actual_value).match(expected_value)
	elif node.has_meta(meta_key):
		var actual_value: Variant = node.get_meta(meta_key)
		if expected_value.is_empty():
			return true
		return str(actual_value).match(expected_value)
	return false


## Depth-first walk of the scene tree, calling visitor on every node.
static func _walk(root: Node, visitor: Callable) -> void:
	visitor.call(root)
	for child: Node in root.get_children():
		_walk(child, visitor)
