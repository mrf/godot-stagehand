## Evaluates GDScript expressions on nodes matched by a selector.
##
## SAFETY NOTE: Expression.execute() runs arbitrary GDScript in the game
## process. This is intentionally unrestricted for automation and testing
## use cases — the MCP server is a local debugging/testing tool, not a
## public API. The trust boundary is the WebSocket connection itself,
## gated by the STAGEHAND_ENABLED activation guard. If you need to
## restrict expression evaluation, add an allowlist or sandbox at this
## layer rather than relying on the MCP client to self-police.
##
## ENGINE SINGLETONS: Core engine singletons (Engine, OS, Time, Input,
## DisplayServer, ProjectSettings, …) are NOT resolvable by Expression on
## their own — Expression only resolves named indices against the
## base_instance and the input_names you pass in. We bind every registered
## engine singleton (Engine.get_singleton_list()) as a named input so that
## expressions like `Engine.get_physics_frames()` or `OS.get_name()` work.
##
## TERNARY / CONDITIONAL LIMITATION (documented, not fixed): Godot's
## `Expression` class does NOT support GDScript conditional expressions
## (`a if cond else b`). Inside a call argument it raises a parse error
## ("Expected ',' or ')'"); at the top level it is WORSE — it silently
## parses only the leading sub-expression and discards the rest, so
## `10 if false else 20` evaluates to `10` (the wrong branch) with no error.
## This is inherent to the engine's Expression parser, which stops after the
## first complete expression and ignores trailing tokens (`"10 garbage"`
## also parses without error). It cannot be fixed here without reimplementing
## the GDScript expression parser. Workaround for callers: avoid ternaries —
## compute the condition and branches in separate evaluate calls, or rewrite
## using boolean arithmetic. Verified against Godot 4.6.2 (2026-06-01).
##
## SCRIPT MEMBERS: Reading script-declared member variables on resolved nodes
## (e.g. `get_node("Foo")._hp`) works in current Godot — Expression's named
## index falls through to Object.get(), which honors GDScript properties,
## including underscore-prefixed ones. The water-wars "Invalid named index"
## report was not reproducible in 4.6.2; no change required here.
class_name StagehandExpressionEvaluator
extends RefCounted

const SELECTOR_ENGINE := preload("res://addons/stagehand/core/selector_engine.gd")
const TREE_SERIALIZER := preload("res://addons/stagehand/core/tree_serializer.gd")

## Lazily-built cache of engine singleton names, parallel to [member _singleton_objects].
static var _singleton_names: PackedStringArray = PackedStringArray()
## Lazily-built cache of engine singleton object references, parallel to [member _singleton_names].
static var _singleton_objects: Array = []
## Guards lazy initialization of the singleton caches.
static var _singletons_ready: bool = false


## Build (once) the parallel name/object arrays for every registered engine
## singleton so they can be bound as Expression inputs.
static func _ensure_singletons() -> void:
	if _singletons_ready:
		return
	_singletons_ready = true

	var names: PackedStringArray = PackedStringArray()
	var objects: Array = []
	for singleton_name: String in Engine.get_singleton_list():
		var singleton: Object = Engine.get_singleton(singleton_name)
		if singleton == null:
			continue
		var _appended: bool = names.append(singleton_name)
		objects.append(singleton)

	_singleton_names = names
	_singleton_objects = objects


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

	_ensure_singletons()

	var expr: Expression = Expression.new()
	var parse_err: Error = expr.parse(expression_str, _singleton_names)
	if parse_err != OK:
		return {"error": "Parse error: %s" % expr.get_error_text()}

	var result: Variant = expr.execute(_singleton_objects, base_node)
	if expr.has_execute_failed():
		return {"error": "Execution error: %s" % expr.get_error_text()}

	return {
		"value": TREE_SERIALIZER._to_json_safe(result),
		"type": type_string(typeof(result)),
	}
