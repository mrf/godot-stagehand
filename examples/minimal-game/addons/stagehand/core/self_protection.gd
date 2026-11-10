class_name StagehandSelfProtection
extends RefCounted
## Keeps the safe remote surface off Stagehand's own state.
##
## The server's safe-mode flag and auth token are ordinary script members, so
## without these rules an authenticated client could set_property its way into
## unsafe mode or read the token back (godot-stagehand-ext-review-2026-11-327t.1).
## See docs/security.md ("Self-protection"):
##   1. No selector-driven property read/write or method call may target a node
##      in [constant PROTECTED_GROUP], or any descendant of one.
##   2. get_property/set_property never touch a `_`-prefixed member or `script`,
##      on any node.

const ERRORS := preload("res://addons/stagehand/core/errors.gd")

## The StagehandServer autoload joins this group in its _ready(). Groups are not
## properties, so nothing on the safe surface can take a node out of it.
const PROTECTED_GROUP: StringName = &"_stagehand_protected"


## True when [param node] or any of its ancestors is in [constant PROTECTED_GROUP].
static func is_protected(node: Node) -> bool:
	var current: Node = node
	while current != null:
		if current.is_in_group(PROTECTED_GROUP):
			return true
		current = current.get_parent()
	return false


## The first protected node in [param nodes], or null when none is.
static func first_protected(nodes: Array[Node]) -> Node:
	for node: Node in nodes:
		if is_protected(node):
			return node
	return null


## True when any segment of [param property] (split on `.`, `:` and `/`) is a
## private `_`-prefixed name or `script`.
static func is_forbidden_property(property: String) -> bool:
	var normalized: String = property.replace(":", ".").replace("/", ".")
	for part: String in normalized.split("."):
		if part.begins_with("_") or part == "script":
			return true
	return false


static func protected_node_error(selector: String, node: Node) -> Dictionary:
	return ERRORS.make(
		ERRORS.NOT_SUPPORTED,
		"Blocked: '%s' belongs to Stagehand itself and cannot be inspected or modified remotely"
			% node.get_path(),
		{
			"selector": selector,
			"node_path": str(node.get_path()),
			"next_action": "Target a node of the game itself; the StagehandServer node and its children are off limits.",
		}
	)


static func forbidden_property_error(selector: String, property: String) -> Dictionary:
	return ERRORS.make(
		ERRORS.NOT_SUPPORTED,
		"Blocked: private ('_'-prefixed) members and 'script' cannot be read or written remotely: %s"
			% property,
		{
			"selector": selector,
			"property": property,
			"next_action": "Read or write a public property instead; expose a public property or getter in the game script if you need this value.",
		}
	)
