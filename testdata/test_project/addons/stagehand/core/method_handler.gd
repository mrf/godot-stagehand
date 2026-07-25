## Handles call_method commands with safety boundaries.
##
## A blocklist prevents calling dangerous methods that could destabilize
## the game or compromise security during automation.
class_name StagehandMethodHandler
extends RefCounted

const ERRORS := preload("res://addons/stagehand/core/errors.gd")
const SELECTOR_ENGINE := preload("res://addons/stagehand/core/selector_engine.gd")
const TREE_SERIALIZER := preload("res://addons/stagehand/core/tree_serializer.gd")

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

	if selector.is_empty():
		return ERRORS.missing_param("selector")
	if method.is_empty():
		return ERRORS.missing_param("method")

	var err: String = _validate_method(method)
	if not err.is_empty():
		return ERRORS.make(ERRORS.NOT_SUPPORTED, err, {
			"method": method,
			"next_action": "Pick a public, non-destructive method; see BLOCKED_METHODS in core/method_handler.gd.",
		})

	var nodes: Array[Node] = SELECTOR_ENGINE.query(tree, selector)
	if nodes.is_empty():
		return ERRORS.node_not_found(selector)

	var args: Array = params.get("args", [])
	var allow_multiple: bool = params.get("allow_multiple", false)

	if not allow_multiple:
		var node: Node = nodes[0]
		if not node.has_method(method):
			return ERRORS.make(
				ERRORS.METHOD_NOT_FOUND,
				"Method not found on node '%s': %s" % [node.get_path(), method],
				{
					"selector": selector,
					"node_path": str(node.get_path()),
					"method": method,
					"next_action": "Call get_property on \"script\" or check the node class to confirm the method name.",
				}
			)
		var result: Variant = node.callv(method, args)
		return {
			"success": true,
			"return_value": TREE_SERIALIZER._to_json_safe(result),
		}

	var results: Array[Dictionary] = []
	for node: Node in nodes:
		if not node.has_method(method):
			return ERRORS.make(
				ERRORS.METHOD_NOT_FOUND,
				"Method not found on node '%s': %s" % [node.get_path(), method],
				{
					"selector": selector,
					"node_path": str(node.get_path()),
					"method": method,
					"next_action": "Every node matched by the selector must expose the method when allow_multiple is set.",
				}
			)
		var result: Variant = node.callv(method, args)
		results.append({
			"node_path": str(node.get_path()),
			"return_value": TREE_SERIALIZER._to_json_safe(result),
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
