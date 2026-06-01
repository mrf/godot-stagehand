## Evaluates GDScript expressions on nodes matched by a selector.
##
## SAFETY NOTE: Expression.execute() runs arbitrary GDScript in the game
## process. This is intentionally unrestricted for automation and testing
## use cases — the MCP server is a local debugging/testing tool, not a
## public API. The trust boundary is the WebSocket connection itself,
## gated by the STAGEHAND_ENABLED activation guard. If you need to
## restrict expression evaluation, add an allowlist or sandbox at this
## layer rather than relying on the MCP client to self-police.
class_name StagehandExpressionEvaluator
extends RefCounted

const SELECTOR_ENGINE := preload("res://addons/stagehand/core/selector_engine.gd")
const TREE_SERIALIZER := preload("res://addons/stagehand/core/tree_serializer.gd")


## Evaluate an expression with the matched node available as `self`.
static func evaluate(tree: SceneTree, params: Dictionary) -> Dictionary:
	var expression_str: String = params.get("expression", "")
	if expression_str.is_empty():
		return {"error": "Missing expression"}

	var context_node: String = params.get("context_node", "")

	# Resolve the base node for expression context. If no context_node is
	# given, use the scene tree root.
	var base_node: Node
	if context_node.is_empty():
		base_node = tree.root
	else:
		var nodes: Array[Node] = SELECTOR_ENGINE.query(tree, context_node)
		if nodes.is_empty():
			return {"error": "Node not found for context_node: %s" % context_node}
		base_node = nodes[0]

	var expr: Expression = Expression.new()
	var parse_err: Error = expr.parse(expression_str)
	if parse_err != OK:
		return {"error": "Parse error: %s" % expr.get_error_text()}

	var result: Variant = expr.execute([], base_node)
	if expr.has_execute_failed():
		return {"error": "Execution error: %s" % expr.get_error_text()}

	return {
		"value": TREE_SERIALIZER._to_json_safe(result),
		"type": type_string(typeof(result)),
	}
