## Handles call_method command.
class_name StagehandMethodHandler
extends RefCounted

const SelectorEngine := preload("res://addons/stagehand/core/selector_engine.gd")
const StagehandTreeSerializer := preload("res://addons/stagehand/core/tree_serializer.gd")

## Call a method on a node matched by a selector.
## Params: {
##   selector: string,
##   method: string,
##   args: Array (optional),
##   allow_multiple: bool (optional, default false)
## }
static func call_method(tree: SceneTree, params: Dictionary) -> Dictionary:
	var selector: String = params.get("selector", "")
	var method_name: String = params.get("method", "")
	var args: Array = params.get("args", []) as Array
	var allow_multiple: bool = params.get("allow_multiple", false)
	
	if selector.is_empty():
		return {"error": "Missing selector"}
	if method_name.is_empty():
		return {"error": "Missing method"}
	
	var nodes: Array[Node] = SelectorEngine.query(tree, selector)
	if nodes.is_empty():
		return {"error": "Node not found for selector: %s" % selector}
	
	if not allow_multiple and nodes.size() > 1:
		return {"error": "Selector matched %d nodes. Use allow_multiple=true to call on all matches." % nodes.size()}
	
	var results := []
	for node: Node in nodes:
		if not node.has_method(method_name):
			return {"error": "Node %s does not have method '%s'" % [node.get_path(), method_name]}
	
	for node: Node in nodes:
		var result = node.callv(method_name, args)
		results.append({
			"node": StagehandTreeSerializer._node_info(node),
			"result": StagehandTreeSerializer._to_json_safe(result),
		})
	
	if not allow_multiple:
		# Return single result (not wrapped in array) for backward compatibility.
		return {
			"success": true,
			"result": results[0].result,
		}
	else:
		return {
			"success": true,
			"results": results,
		}