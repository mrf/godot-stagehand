## Builds a semantic, role-annotated view of the UI for automation agents.
##
## [b]What this is, and what it deliberately is not.[/b]
##
## Godot 4.5+ ships AccessKit integration, but its GDScript surface is a
## strictly [i]write-only[/i] push API: the engine calls
## [code]DisplayServer.accessibility_update_set_role()[/code] (and ~75 sibling
## setters) to push node state [i]into[/i] the platform screen-reader adapter.
## Nothing reads back out. Verified against Godot 4.6.2 by ClassDB
## introspection plus a runtime probe:
##   - there is no role getter on [Node], [Control], or [DisplayServer];
##   - [code]Node.get_accessibility_element()[/code] returns an [b]invalid[/b] RID
##     unless a screen reader is actually attached, so it is always invalid in
##     headless/CI runs.
##
## So the real AccessKit tree cannot be queried from GDScript. Rather than
## invent APIs, this module reports a tree [b]derived[/b] from what genuinely is
## readable — the [Control] class hierarchy, the author-set
## [code]accessibility_name[/code]/[code]accessibility_description[/code]
## properties, and live focus/press/disable state. That mirrors what the engine
## itself does internally (it also derives the role from the Control subclass
## before pushing it), and roles are emitted in the engine's own canonical
## [code]DisplayServer.ROLE_*[/code] vocabulary rather than a made-up one.
##
## Every response is tagged [code]source: "derived"[/code] so no caller can
## mistake it for the real accessibility tree.
class_name StagehandAccessibilityTree
extends RefCounted

const ERRORS := preload("res://addons/stagehand/core/errors.gd")

## Godot version that introduced the AccessibilityRole enum.
const MIN_MAJOR: int = 4
const MIN_MINOR: int = 5

## Role reported for anything with no defensible mapping.
const ROLE_UNKNOWN: String = "unknown"

## Ordered class → role mapping, most-derived class first (the first
## [method Node.is_class] hit wins). Every role string here is asserted against
## the live engine enum by [method derivable_role_names]'s test, so a typo or an
## invented role fails the suite rather than reaching an agent.
const ROLE_BY_CLASS: Array[Array] = [
	# BaseButton family — subclasses before Button itself.
	["CheckBox", "check_box"],
	["CheckButton", "check_button"],
	["LinkButton", "link"],
	["ColorPickerButton", "color_picker"],
	["MenuButton", "menu"],
	["OptionButton", "button"],
	["TextureButton", "button"],
	["Button", "button"],
	["BaseButton", "button"],
	# Text.
	["CodeEdit", "multiline_text_field"],
	["TextEdit", "multiline_text_field"],
	["LineEdit", "text_field"],
	["RichTextLabel", "static_text"],
	["Label", "static_text"],
	# Range family.
	["SpinBox", "spin_button"],
	["ProgressBar", "progress_indicator"],
	["ScrollBar", "scroll_bar"],
	["Slider", "slider"],
	# Collections.
	["Tree", "tree"],
	["ItemList", "list"],
	["TabBar", "tab_bar"],
	["TabContainer", "tab_panel"],
	["MenuBar", "menu_bar"],
	["PopupMenu", "menu"],
	# Media.
	["TextureRect", "image"],
	["VideoStreamPlayer", "video"],
	["ColorPicker", "color_picker"],
	# Structure — least specific last.
	["ScrollContainer", "scroll_view"],
	["SplitContainer", "splitter"],
	["AcceptDialog", "dialog"],
	["Window", "window"],
	["PanelContainer", "panel"],
	["Panel", "panel"],
	["Container", "container"],
	["Control", "container"],
]


## True when the running engine exposes the AccessibilityRole vocabulary (4.5+).
static func is_supported() -> bool:
	var version: Dictionary = Engine.get_version_info()
	var major: int = version.get("major", 0)
	var minor: int = version.get("minor", 0)
	if major > MIN_MAJOR:
		return true
	return major == MIN_MAJOR and minor >= MIN_MINOR


## Human-readable reason this module cannot report roles on this engine.
static func unsupported_reason() -> String:
	var version: Dictionary = Engine.get_version_info()
	var running: String = version.get("string", "unknown")
	return (
		"accessibility roles require Godot %d.%d or newer (running %s)"
		% [MIN_MAJOR, MIN_MINOR, running]
	)


## Every role name the engine actually defines, lowercased and without the
## [code]ROLE_[/code] prefix. Empty on engines predating the enum.
static func engine_role_names() -> PackedStringArray:
	var names: PackedStringArray = PackedStringArray()
	var constants: PackedStringArray = ClassDB.class_get_enum_constants(
		"DisplayServer", "AccessibilityRole", true
	)
	for constant: String in constants:
		@warning_ignore("return_value_discarded")
		names.append(_role_constant_to_name(constant))
	return names


## Every distinct role name this module's mapping can emit.
static func derivable_role_names() -> PackedStringArray:
	var names: PackedStringArray = PackedStringArray([ROLE_UNKNOWN])
	for entry: Array in ROLE_BY_CLASS:
		var role: String = entry[1]
		if not names.has(role):
			@warning_ignore("return_value_discarded")
			names.append(role)
	return names


## Derive the accessibility role for [param node] from its class hierarchy.
static func role_for(node: Node) -> String:
	if node == null:
		return ROLE_UNKNOWN
	for entry: Array in ROLE_BY_CLASS:
		var class_key: String = entry[0]
		if node.is_class(class_key):
			var role: String = entry[1]
			return role
	return ROLE_UNKNOWN


## Full JSON-safe response, including the version gate and the
## [code]source: "derived"[/code] honesty tag.
static func build_response(root_node: Node, max_depth: int = 10) -> Dictionary:
	if not is_supported():
		return ERRORS.make(ERRORS.NOT_SUPPORTED, unsupported_reason(), {
			"source": "derived",
			"supported": false,
			"next_action": "Run the game on Godot %d.%d or newer to read accessibility roles." % [
				MIN_MAJOR, MIN_MINOR,
			],
		})
	var tree: Dictionary = build(root_node, max_depth)
	var version: Dictionary = Engine.get_version_info()
	var version_string: String = version.get("string", "")
	return {
		"source": "derived",
		"supported": true,
		"godot_version": version_string,
		"nodes": [tree],
		"count": _count_nodes(root_node),
	}


## Build the derived accessibility subtree rooted at [param node].
## [param max_depth] limits recursion; the root itself does not count.
static func build(node: Node, max_depth: int = 10) -> Dictionary:
	return _build_entry(node, 0, max_depth)


static func _build_entry(node: Node, depth: int, max_depth: int) -> Dictionary:
	var entry: Dictionary = {
		"role": role_for(node),
		"name": accessible_name(node),
		"value": accessible_value(node),
		"description": accessible_description(node),
		"path": String(node.get_path()),
		"class": node.get_class(),
		"state": state_for(node),
	}
	if depth < max_depth:
		var children: Array[Dictionary] = []
		for child: Node in node.get_children():
			children.append(_build_entry(child, depth + 1, max_depth))
		entry["children"] = children
	else:
		entry["children"] = [] as Array[Dictionary]
	return entry


## Accessible name, in precedence order:
## author-set [code]accessibility_name[/code] → visible text → placeholder →
## node name. Note the engine's own getter does [b]not[/b] fall back to text,
## so the fallback chain is this module's contribution.
static func accessible_name(node: Node) -> String:
	var explicit: String = _call_string_method(node, "get_accessibility_name")
	if not explicit.is_empty():
		return explicit
	var text: String = _read_string_property(node, "text")
	if not text.is_empty():
		return text
	var placeholder: String = _read_string_property(node, "placeholder_text")
	if not placeholder.is_empty():
		return placeholder
	var title: String = _read_string_property(node, "title")
	if not title.is_empty():
		return title
	return String(node.name)


## Author-set accessibility description, or "" when unset / unavailable.
static func accessible_description(node: Node) -> String:
	return _call_string_method(node, "get_accessibility_description")


## The control's current mutable value — editable text or numeric range value.
## Static labels expose their text as the [i]name[/i], not the value.
static func accessible_value(node: Node) -> String:
	if node is Range:
		var range_node: Range = node as Range
		return String.num(range_node.value)
	if node is LineEdit:
		var line_edit: LineEdit = node as LineEdit
		return line_edit.text
	if node is TextEdit:
		var text_edit: TextEdit = node as TextEdit
		return text_edit.text
	return ""


## Live interaction state, built only from readable properties.
static func state_for(node: Node) -> Dictionary:
	var state: Dictionary = {}
	if node is Control:
		var control: Control = node as Control
		state["hidden"] = not control.is_visible_in_tree()
		state["focused"] = control.has_focus()
		state["focusable"] = control.focus_mode != Control.FOCUS_NONE
	else:
		state["hidden"] = false
		state["focused"] = false
		state["focusable"] = false
	if node is BaseButton:
		var button: BaseButton = node as BaseButton
		state["disabled"] = button.disabled
		state["pressed"] = button.button_pressed
		if button.toggle_mode:
			state["checked"] = button.button_pressed
	if node is LineEdit:
		var line_edit: LineEdit = node as LineEdit
		state["editable"] = line_edit.editable
	if node is TextEdit:
		var text_edit: TextEdit = node as TextEdit
		state["editable"] = text_edit.editable
	return state


## Call a no-arg String-returning method [b]dynamically[/b], returning "" when it
## does not exist on this engine build.
##
## This indirection is load-bearing, not defensive noise:
## [code]get_accessibility_name[/code]/[code]get_accessibility_description[/code]
## only exist on Godot 4.5+, and a statically-typed call to them would make this
## script fail to compile on 4.3/4.4 — which the addon still supports and which
## the CI compat matrix builds. A host project on 4.4 would break on install.
static func _call_string_method(node: Node, method: String) -> String:
	if node == null or not node.has_method(method):
		return ""
	var value: Variant = node.call(method)
	return _variant_to_string(value)


## "ROLE_CHECK_BOX" → "check_box".
static func _role_constant_to_name(constant: String) -> String:
	return constant.trim_prefix("ROLE_").to_lower()


## Read a String property from a node without assuming it exists.
static func _read_string_property(node: Node, property: String) -> String:
	if not (property in node):
		return ""
	var value: Variant = node.get(property)
	return _variant_to_string(value)


## Narrow a [Variant] to [String], accepting [StringName] but nothing else.
## The typed locals are what keep this strict-warning clean: `is` does not
## narrow a Variant for the analyzer, so `String(value)` would be an unsafe
## call argument.
static func _variant_to_string(value: Variant) -> String:
	if value is String:
		var text: String = value
		return text
	if value is StringName:
		var name_value: StringName = value
		return String(name_value)
	return ""


static func _count_nodes(root: Node) -> int:
	var count: int = 1
	for child: Node in root.get_children():
		count += _count_nodes(child)
	return count
