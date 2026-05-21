## Handles call_method commands with safety boundaries.
##
## A blocklist prevents calling dangerous methods that could destabilize
## the game or compromise security during automation.
class_name StagehandMethodHandler
extends RefCounted

const SelectorEngine := preload("res://addons/stagehand/core/selector_engine.gd")
const StagehandTreeSerializer := preload("res://addons/stagehand/core/tree_serializer.gd")

## Methods that are always blocked regardless of context.
const BLOCKED_METHODS: PackedStringArray = [
	"free",
	"queue_free",
	"set_script",
	"add_child",
	"remove_child",
	"queue_redraw",
	"notification",
	"propagate_notification",
	"set_process",
	"set_physics_process",
]


## Call a method on a node matched by a selector.
## When allow_multiple is true, calls the method on all matched nodes and returns
## a "results" array with one entry per node; otherwise operates on the first match.
static func call_method(tree: SceneTree, params: Dictionary) -> Dictionary:
	var selector: String = params.get("selector", "")
	var method: String = params.get("method", "")

	if selector.is_empty() or method.is_empty():
		return {"error": "Missing selector or method"}

	var err: String = _validate_method(method)
	if not err.is_empty():
		return {"error": err}

	var nodes: Array[Node] = SelectorEngine.query(tree, selector)
	if nodes.is_empty():
		return {"error": "Node not found for selector: %s" % selector}

	var args: Array = params.get("args", [])
	var allow_multiple: bool = params.get("allow_multiple", false)

	if not allow_multiple:
		var node: Node = nodes[0]
		if not node.has_method(method):
			return {"error": "Method not found: %s" % method}
		var result: Variant = node.callv(method, args)
		return {
			"success": true,
			"return_value": StagehandTreeSerializer._to_json_safe(result),
		}

	var results: Array[Dictionary] = []
	for node: Node in nodes:
		if not node.has_method(method):
			return {"error": "Method not found on node '%s': %s" % [node.get_path(), method]}
		var result: Variant = node.callv(method, args)
		results.append({
			"node_path": str(node.get_path()),
			"return_value": StagehandTreeSerializer._to_json_safe(result),
		})

	return {
		"success": true,
		"results": results,
	}


## Validate a method name against safety rules. Returns an error string,
## or empty string if the method is allowed.
static func _validate_method(method: String) -> String:
	if method.begins_with("_"):
		return "Blocked: private/lifecycle methods (starting with '_') cannot be called"

	if method in BLOCKED_METHODS:
		return "Blocked: '%s' is a destructive method and cannot be called remotely" % method

	return ""
