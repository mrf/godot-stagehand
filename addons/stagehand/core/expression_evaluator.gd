## Handles evaluate command.
## WARNING: Arbitrary GDScript evaluation is dangerous.
## Only enabled when Stagehand is explicitly enabled (already guarded by server activation).
class_name StagehandExpressionEvaluator
extends RefCounted

const StagehandTreeSerializer := preload("res://addons/stagehand/core/tree_serializer.gd")

## Evaluate a GDScript expression.
## Params: {
##   expression: string,
##   context_node: string (optional node path to use as 'self')
## }
static func evaluate(tree: SceneTree, params: Dictionary) -> Dictionary:
	var expression_str: String = params.get("expression", "")
	if expression_str.is_empty():
		return {"error": "Missing expression"}
	
	var context_node_path: String = params.get("context_node", "")
	var context_node: Node = null
	if not context_node_path.is_empty():
		var nodes: Array[Node] = tree.root.get_node_or_null(NodePath(context_node_path))
		if nodes.is_empty() or nodes[0] == null:
			return {"error": "Context node not found: %s" % context_node_path}
		context_node = nodes[0]
	else:
		# If no context node, use the root.
		context_node = tree.root
	
	var expr := Expression.new()
	var parse_err := expr.parse(expression_str, [])
	if parse_err != OK:
		return {"error": "Expression parse error: %s" % expr.get_error_text()}
	
	var result = expr.execute([], context_node, true)
	if expr.has_execute_failed():
		return {"error": "Expression execution failed: %s" % expr.get_error_text()}
	
	return {
		"result": StagehandTreeSerializer._to_json_safe(result),
		"type": type_string(typeof(result)),
	}