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
}


static func query(tree: SceneTree, selector: String) -> Array[Node]:
	# Check if selector is a chained selector (contains >>)
	if selector.contains(">>"):
		return query_chained_selector(tree, selector)
	else:
		# For backward compatibility, use simple parsing
		var parsed := parse(selector)
		if parsed.is_empty():
			return [] as Array[Node]
		return _resolve(tree, parsed)


# Split and process a chained selector string
static func query_chained_selector(tree: SceneTree, selector_str: String) -> Array[Node]:
	var parts := selector_str.split(">>", false)
	
	# Parse each part of the chain
	var parsed_parts := [] as Array[Dictionary]
	for part: String in parts:
		var trimmedPart := part.strip_edges()
		if trimmedPart.is_empty():
			push_error("Invalid empty selector part in chain: %s" % selector_str)
			return [] as Array[Node]
		var parsed := parse(trimmedPart)  # Reuse existing parsing logic
		if parsed.is_empty():
			push_error("Failed to parse selector part: %s" % trimmedPart)
			return [] as Array[Node]
		parsed_parts.append(parsed)
	
	# Start with root for the first selector OR find initial matches
	var current_matches: Array[Node] = []
	if parsed_parts[0].type == SelectorType.PATH:
		current_matches = _resolve_path(tree, parsed_parts[0].value)
	else:
		current_matches = _resolve(tree, parsed_parts[0])
	
	# Process the rest of the chain elements
	for i in range(1, parsed_parts.size()):
		var next_result: Array[Node] = []
		var current_selector = parsed_parts[i]
		for node in current_matches:
			# Scope the next selector only to the subtree of this node
			var scoped_matches = _resolve_scoped(node, current_selector)
			next_result.append_array(scoped_matches)
		current_matches = next_result.duplicate()  # Update matches
	
	return current_matches


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

	if trimmed.begins_with("text:"):
		var text_content := trimmed.substr(5)
		if text_content.is_empty():
			return {}
		return {type = SelectorType.TEXT, value = text_content}

	if trimmed.begins_with("meta:"):
		var meta_data := trimmed.substr(5)
		if meta_data.is_empty():
			return {}
		return {type = SelectorType.META, value = meta_data}

	if trimmed.begins_with("unique:"):
		var unique_id := trimmed.substr(7)
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
		SelectorType.META:
			return _resolve_meta(tree, value)
		SelectorType.UNIQUE:
			return _resolve_unique(tree, value)

	return [] as Array[Node]


# Resolve selector but only within and under a given node (for chaining)
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
		SelectorType.META:
			return _resolve_meta_from_parent(parent_node, value)
		SelectorType.UNIQUE:
			return _resolve_unique_from_parent(parent_node, value)

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


static func _resolve_text(tree: SceneTree, text_pattern: String) -> Array[Node]:
	var root := tree.root
	if root == null:
		return [] as Array[Node]
	var results: Array[Node] = []
	_walk(root, func(node: Node) -> void:
		# Check various text-related properties across different control types
		var node_text := ""
		
		# Get text content based on node type
		if node.has_method("get_text"):
			node_text = node.get_text()
		elif node.has_method("get_label"):
			node_text = node.get_label()
		elif node.has_method("get_title"):
			node_text = node.get_title()
		elif "text" in node:
			node_text = node.text
		elif "label" in node:
			node_text = node.label
		elif "title" in node:
			node_text = node.title
		
		if node_text != "" and _matches_pattern(node_text, text_pattern):
			results.append(node)
	)
	return results


static func _resolve_meta(tree: SceneTree, meta_expr: String) -> Array[Node]:
	var root := tree.root
	if root == null:
		return [] as Array[Node]
	var results: Array[Node] = []
	
	# Parse meta expression (key=value or key)
	var meta_key := meta_expr
	var expected_value: String = ""
	if meta_expr.contains("="):
		var parts := meta_expr.split("=", false, 1)
		meta_key = parts[0]
		expected_value = parts[1] if parts.size() > 1 else ""
	
	_walk(root, func(node: Node) -> void:
		if meta_key in node:
			var actual_value = node[meta_key]
			if expected_value.is_empty():
				results.append(node)
			elif str(actual_value).match(expected_value):
				results.append(node)
		elif node.has_meta(meta_key):
			var actual_value = node.get_meta(meta_key)
			if expected_value.is_empty():
				results.append(node)
			elif str(actual_value).match(expected_value):
				results.append(node)
	)
	return results


static func _resolve_unique(tree: SceneTree, unique_identifier: String) -> Array[Node]:
	var root := tree.root
	if root == null:
		return [] as Array[Node]
	var results: Array[Node] = []
	_walk(root, func(node: Node) -> void:
		# Look for unique identifiers like name patterns, specific properties, etc.
		# Could be node.name, node.hint_tooltip, custom properties, etc.
		var identifier_matches := false
		
		# Match against node name first
		if _matches_pattern(node.name, unique_identifier):
			identifier_matches = true
		# Check other likely unique identifiers
		elif "hint_tooltip" in node and node.hint_tooltip.match(unique_identifier):
			identifier_matches = true
		elif "placeholder_text" in node and node.placeholder_text.match(unique_identifier):
			identifier_matches = true
		elif "accessible_role" in node and str(node.accessible_role).to_lower().match(unique_identifier.to_lower()):
			identifier_matches = true
		
		if identifier_matches:
			results.append(node)
	)
	return results


# Scoped resolution helpers

static func _resolve_path_from_parent(parent: Node, path: String) -> Array[Node]:
	if not path.begins_with("/"):
		# Relative path from parent
		var node := parent.get_node_or_null(NodePath(path))
		if node == null:
			return [] as Array[Node]
		return [node] as Array[Node]
	else:
		# Absolute path, return empty for parent-scoped search since root is defined
		return [] as Array[Node]


static func _resolve_name_from_parent(parent: Node, pattern: String) -> Array[Node]:
	var results: Array[Node] = []
	# Use find_children from the parent (this already searches descendants)
	results.assign(parent.find_children(pattern))
	return results


static func _resolve_class_from_parent(parent: Node, class_name_: String) -> Array[Node]:
	var results: Array[Node] = []
	_walk(parent, func(node: Node) -> void:
		if node != parent and node.is_class(class_name_):  # Exclude the parent node itself
			results.append(node)
	)
	return results


static func _resolve_group_from_parent(parent: Node, group_name: String) -> Array[Node]:
	# Get all nodes in the group, then filter for descendants of parent
	var all_nodes_in_group = parent.tree.get_nodes_in_group(group_name)
	var results: Array[Node] = []
	for node in all_nodes_in_group:
		if _is_ancestor_of(node, parent) and node != parent:  # node is child/descendant of parent, exclude parent itself
			results.append(node)
	return results


static func _resolve_text_from_parent(parent: Node, text_pattern: String) -> Array[Node]:
	var results: Array[Node] = []
	_walk(parent, func(node: Node) -> void:
		if node != parent:  # Exclude parent node itself
			var node_text := ""
			if node.has_method("get_text"):
				node_text = node.get_text()
			elif node.has_method("get_label"):
				node_text = node.get_label()
			elif node.has_method("get_title"):
				node_text = node.get_title()
			elif "text" in node:
				node_text = node.text
			elif "label" in node:
				node_text = node.label
			elif "title" in node:
				node_text = node.title
			
			if node_text != "" and _matches_pattern(node_text, text_pattern):
				results.append(node)
	)
	return results


static func _resolve_meta_from_parent(parent: Node, meta_expr: String) -> Array[Node]:
	var results: Array[Node] = []
	
	# Parse meta expression (key=value or key)
	var meta_key := meta_expr
	var expected_value: String = ""
	if meta_expr.contains("="):
		var parts := meta_expr.split("=", false, 1)
		meta_key = parts[0]
		expected_value = parts[1] if parts.size() > 1 else ""
	
	_walk(parent, func(node: Node) -> void:
		if node != parent:  # Exclude parent node itself
			if meta_key in node:
				var actual_value = node[meta_key]
				if expected_value.is_empty():
					results.append(node)
				elif str(actual_value).match(expected_value):
					results.append(node)
			elif node.has_meta(meta_key):
				var actual_value = node.get_meta(meta_key)
				if expected_value.is_empty():
					results.append(node)
				elif str(actual_value).match(expected_value):
					results.append(node)
	)
	return results


static func _resolve_unique_from_parent(parent: Node, unique_identifier: String) -> Array[Node]:
	var results: Array[Node] = []
	_walk(parent, func(node: Node) -> void:
		if node != parent:  # Exclude parent node itself
			var identifier_matches := false
			
			# Match against node name first
			if _matches_pattern(node.name, unique_identifier):
				identifier_matches = true
			# Check other likely unique identifiers
			elif "hint_tooltip" in node and node.hint_tooltip.match(unique_identifier):
				identifier_matches = true
			elif "placeholder_text" in node and node.placeholder_text.match(unique_identifier):
				identifier_matches = true
			elif "accessible_role" in node and str(node.accessible_role).to_lower().match(unique_identifier.to_lower()):
				identifier_matches = true
			
			if identifier_matches:
				results.append(node)
	)
	return results


# Check if first node is descendant of second node
static func _is_ancestor_of(descendant: Node, ancestor: Node) -> bool:
	var current = descendant.get_parent()
	while current:
		if current == ancestor:
			return true
		current = current.get_parent()
	return false


# Helper function to check if string matches pattern (glob or exact)
static func _matches_pattern(text: String, pattern: String) -> bool:
	if text.is_empty() or pattern.is_empty():
		return false
	
	# If pattern contains wildcards, use match
	if pattern.contains("*") or pattern.contains("?") or pattern.contains("["):
		return text.match(pattern)
	else:
		# Case-insensitive substring match
		return text.to_lower().contains(pattern.to_lower())


## Depth-first walk of the scene tree, calling visitor on every node.
static func _walk(root: Node, visitor: Callable) -> void:
	visitor.call(root)
	for child: Node in root.get_children():
		_walk(child, visitor)