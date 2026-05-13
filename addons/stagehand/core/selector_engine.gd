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
	var chains := parse_chain(selector)
	if chains.is_empty():
		return [] as Array[Node]
	return _resolve_chain(tree, chains)


static func parse_chain(selector: String) -> Array[Dictionary]:
	var trimmed := selector.strip_edges()
	if trimmed.is_empty():
		return [] as Array[Dictionary]
	
	# Split by >> for chaining
	var parts := trimmed.split(">>", false)
	var result := [] as Array[Dictionary]
	
	for part in parts:
		var part_stripped := part.strip_edges()
		if part_stripped.is_empty():
			push_error("Invalid selector chain: empty part")
			return [] as Array[Dictionary]
	
		var parsed_part := _parse_single_selector(part_stripped)
		if parsed_part.is_empty():
			push_error("Failed to parse selector part: " + part_stripped)
			return [] as Array[Dictionary]
		result.append(parsed_part)
	
	return result


static func _parse_single_selector(text: String) -> Dictionary:
	var trimmed := text.strip_edges()
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
		var text_value := trimmed.substr(5)
		if text_value.is_empty():
			return {}
		return {type = SelectorType.TEXT, value = text_value}
		
	if trimmed.begins_with("meta:"):
		var meta_value := trimmed.substr(5)
		if meta_value.is_empty():
			return {}
		return {type = SelectorType.META, value = meta_value}
		
	if trimmed.begins_with("unique:"):
		var unique_value := trimmed.substr(7)
		if unique_value.is_empty():
			return {}
		return {type = SelectorType.UNIQUE, value = unique_value}

	# No recognized prefix — treat as exact node path.
	return {type = SelectorType.PATH, value = trimmed}


# Resolve a chain of selectors by applying each on the results of the previous one
static func _resolve_chain(tree: SceneTree, chains: Array[Dictionary]) -> Array[Node]:
	if chains.is_empty():
		return [] as Array[Node]
	
	# Start with the whole tree as nodes to consider
	var current_nodes := [tree.root]
	var final_results := [] as Array[Node]
	
	for i in range(chains.size()):
		var selector := chains[i]
		var intermediate_results := [] as Array[Node]
		
		# Apply the current selector to each current_node
		for starting_node in current_nodes:
			var selector_results := [] as Array[Node]
			match selector.type:
				SelectorType.PATH:
					if starting_node == tree.root:
						selector_results += _resolve_path(tree, selector.value)
					else:
						selector_results += _resolve_path_from(starting_node, selector.value)
				SelectorType.NAME:
					if starting_node == tree.root:
						selector_results += _resolve_name(tree, selector.value)  # search from root first
					else:
						selector_results += _resolve_name_from(starting_node, selector.value)
				SelectorType.CLASS:
					if starting_node == tree.root:
						selector_results += _resolve_class(tree, selector.value)
					else:
						selector_results += _resolve_class_from(starting_node, selector.value)
				SelectorType.GROUP:
					if starting_node == tree.root:
						selector_results += _resolve_group(tree, selector.value)
					else:
						selector_results += _resolve_group_from(starting_node, selector.value)
				SelectorType.TEXT:
					if starting_node == tree.root:
						selector_results += _resolve_text(tree, selector.value)
					else:
						selector_results += _resolve_text_from(starting_node, selector.value)
				SelectorType.META:
					if starting_node == tree.root:
						selector_results += _resolve_meta(tree, selector.value)
					else:
						selector_results += _resolve_meta_from(starting_node, selector.value)
				SelectorType.UNIQUE:
					if starting_node == tree.root:
						selector_results += _resolve_unique(tree, selector.value)
					else:
						selector_results += _resolve_unique_from(starting_node, selector.value)
				_>:
					push_error("Unknown selector type: " + str(selector.type))
			
			intermediate_results += selector_results
		
		if i == 0: # For first selector, set initial results
			final_results = intermediate_results
		else:  # For subsequent selectors, use as filter context
			final_results = intermediate_results
		
		# Update current_nodes for next iteration
		if final_results.is_empty():
			push_warning("No results for selector at index " + str(i) + ". Aborting chain.")
			break
		current_nodes = final_results
	
	return final_results


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


## Helper functions for scoped resolution within a subtree
static func _resolve_path_from(scope_node: Node, path: String) -> Array[Node]:
	# If we have a relative path from the scope, try it; otherwise check if it's absolute
	if path.begins_with(".") or path.begins_with("../"):
		var node := scope_node.get_node_or_null(NodePath(path))
		if node == null:
			return [] as Array[Node]
		return [node] as Array[Node]
	elif path.begins_with("/"):
		# If it's absolute, resolve from root
		var root = scope_node.get_tree().root
		var node := root.get_node_or_null(NodePath(path))
		if node == null:
			return [] as Array[Node]
		return [node] as Array[Node]
	else:
		# If relative (no / prefix), search within scope
		var node = scope_node.get_node_or_null(NodePath(path))
		if node != null:
			return [node] as Array[Node]
		# Also try searching with ./ prefix
		var maybe_node = scope_node.get_node_or_null(NodePath("./" + path))
		if maybe_node != null:
			return [maybe_node] as Array[Node]
		return [] as Array[Node]


static func _resolve_name_from(scope_node: Node, pattern: String) -> Array[Node]:
	# Use find_children on the scope node to search within its subtree
	var results: Array[Node] = [] as Array[Node]
	results.assign(scope_node.find_children(pattern))
	return results


static func _resolve_class_from(scope_node: Node, class_name_: String) -> Array[Node]:
	var results: Array[Node] = [] as Array[Node]
	_walk_from(scope_node, func(node: Node) -> void:
		if node.is_class(class_name_):
			results.append(node)
	)
	return results


static func _resolve_group_from(scope_node: Node, group_name: String) -> Array[Node]:
	# Since groups exist globally, we first get the full list then filter to descendants
	var all_nodes_in_group: Array[Node] 
	all_nodes_in_group.assign(scope_node.get_tree().get_nodes_in_group(group_name))
	var results: Array[Node] = [] as Array[Node]
	for node in all_nodes_in_group:
		if is_descendant_of_scope(scope_node, node):
			results.append(node)
	return results


static func _resolve_text_from(scope_node: Node, text_content: String) -> Array[Node]:
	var results: Array[Node] = [] as Array[Node]
	_walk_from(scope_node, func(node: Node) -> void:
		# Get text from common UI controls that have text content
		var extracted_text := extract_node_text(node)
		if extracted_text.find(text_content) != -1:
			results.append(node)
	)
	return results


static func _resolve_meta_from(scope_node: Node, meta_spec: String) -> Array[Node]:
	var results: Array[Node] = [] as Array[Node]
	# Handle meta format "key=value" where only key can exist "key"
	var parts := meta_spec.split("=")
	if parts.size() > 0:
		var key = parts[0]
		if parts.size() > 1:
			var value = parts[1]
			_walk_from(scope_node, func(node: Node) -> void:
				if node.has_meta(key) and str(node.get_meta(key)) == value:
					results.append(node)
			)
		else:
			_walk_from(scope_node, func(node: Node) -> void:
				if node.has_meta(key):
					results.append(node)
			)
	return results


static func _resolve_unique_from(scope_node: Node, unique_hint: String) -> Array[Node]:
	var results: Array[Node] = [] as Array[Node]
	# Search for nodes with unique characteristics
	_walk_from(scope_node, func(node: Node) -> void:
		# A "unique" node could be identified by various properties
		# 1. Has a unique name across its path hierarchy
		# 2. Is the only node of its class in its parent
		# 3. Contains unique text not found in other siblings
		# 4. Has a specific "data-testid", "uid", or other identifying property
		if is_unique_node_in_context(node, scope_node):
			results.append(node)
	)
	return results


## Helper functions for new selectors
static func _resolve_text(tree: SceneTree, text_content: String) -> Array[Node]:
	var root := tree.root
	if root == null:
		return [] as Array[Node]
	var results: Array[Node] = [] as Array[Node]
	_walk(root, func(node: Node) -> void:
		var extracted_text := extract_node_text(node)
		if extracted_text.find(text_content) != -1:
			results.append(node)
	)
	return results


static func _resolve_meta(tree: SceneTree, meta_spec: String) -> Array[Node]:
	var root := tree.root
	if root == null:
		return [] as Array[Node]
	var results: Array[Node] = [] as Array[Node]
	# Handle meta format "key=value" where only key can exist "key"
	var parts := meta_spec.split("=")
	if parts.size() > 0:
		var key = parts[0]
		if parts.size() > 1:
			var value = parts[1]
			_walk(root, func(node: Node) -> void:
				if node.has_meta(key) and str(node.get_meta(key)) == value:
					results.append(node)
			)
		else:
			_walk(root, func(node: Node) -> void:
				if node.has_meta(key):
					results.append(node)
			)
	return results


static func _resolve_unique(tree: SceneTree, unique_hint: String) -> Array[Node]:
	var root := tree.root
	if root == null:
		return [] as Array[Node]
	var results: Array[Node] = [] as Array[Node]
	_walk(root, func(node: Node) -> void:
		if is_unique_node_in_context(node, root): # pass full tree root as comparison scope
			results.append(node)
	)
	return results


## Extract text content from common UI nodes
static func extract_node_text(node: Node) -> String:
	var text := ""
	if node.has_method("get_text"):
		text = node.call("get_text")
	elif node.has_method("get_tooltip"):
		text += " " + node.call("get_tooltip")
	elif node.has_method("get_title"):
		text += " " + node.call("get_title")
	elif node.has_signal("text_changed"):  # Likely a text control
		var maybe_text_prop = node.get("text")
		if maybe_text_prop is String:
			text = maybe_text_prop
	elif node.has_method("get_label"):
		text = node.call("get_label")
	
	# Additional text extraction from common properties
	if node.has("placeholder_text"):
		var placeholder = node.get("placeholder_text")
		if placeholder is String: text += " " + placeholder
	elif node.has("hint_tooltip"):
		var hint = node.get("hint_tooltip")
		if hint is String: text += " " + hint
	
	text = text.strip_edges().strip_escapes().trim_prefix("").trim_suffix("")
	return text


## Check if a node is a descendant of the given scope
static func is_descendant_of_scope(scope_node: Node, node: Node) -> bool:
	var parent := node.get_parent()
	while parent != null:
		if parent == scope_node:
			return true
		parent = parent.get_parent()
	return false


## Check if a node is "unique" in the context of its parent
static func is_unique_node_in_context(node: Node, root: Node) -> bool:
	# Consider it unique if:
	# 1. The unique hint matches something about the specific node
	# 2. There are other conditions to determine uniqueness based on hint

	# For now we'll consider a node unique if it has unique properties
	var name_is_unique := true
	var parent = node.get_parent()
	if parent != null:
		for sibling in parent.get_children():
			if sibling != node and sibling.name == node.name:
				name_is_unique = false
	
	# Or if it has a unique data-testid or similar identifier 
	var metadata_has_unique_key = node.has_meta("data-testid") or node.has_meta("uid") or node.has_meta("test_id")
	 
	 return name_is_unique or metadata_has_unique_key 


## Depth-first walk starting from a specific node, calling visitor on every node.
static func _walk_from(root: Node, visitor: Callable) -> void:
	visitor.call(root)
	for child: Node in root.get_children():
		_walk_from(child, visitor)


## Depth-first walk of the scene tree, calling visitor on every node.
static func _walk(root: Node, visitor: Callable) -> void:
	visitor.call(root)
	for child: Node in root.get_children():
		_walk(child, visitor)
