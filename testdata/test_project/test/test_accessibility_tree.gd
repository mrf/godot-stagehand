# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for StagehandAccessibilityTree — the derived semantic/role view.
##
## Godot's AccessKit tree is a write-only push API with no GDScript read path
## (see the class docs), so these tests pin the DERIVED behaviour: class
## hierarchy → role mapping in the engine's canonical DisplayServer.ROLE_*
## vocabulary, accessible-name precedence, and interaction state.
##
## Role derivation needs the DisplayServer.AccessibilityRole enum, which only
## exists on Godot 4.5+, so role assertions are gated on is_supported().

## Preloaded rather than referenced by its `class_name` — see the
## preload-over-class_name convention in docs/gdscript-testing.md.
const ACCESSIBILITY_TREE := preload("res://addons/stagehand/core/accessibility_tree.gd")

var _root: Control


func before_test() -> void:
	_root = auto_free(Control.new())
	_root.name = "AxRoot"
	add_child(_root)


func _attach(node: Node, node_name: String) -> void:
	node.name = node_name
	_root.add_child(node)


# ── Variant-narrowing helpers ────────────────────────────────────────────
# The strict project treats unsafe_call_argument as an error, so Variant values
# pulled out of result Dictionaries are narrowed here rather than at each site.

func _as_dict(value: Variant) -> Dictionary:
	if value is Dictionary:
		return value
	return {}


func _as_array(value: Variant) -> Array:
	if value is Array:
		return value
	return []


func _str_field(entry: Dictionary, key: String) -> String:
	var value: Variant = entry.get(key, "")
	if value is String:
		return value
	return ""


func _state_flag(entry: Dictionary, key: String) -> bool:
	var state: Dictionary = _as_dict(entry.get("state", {}))
	var flag: Variant = state.get(key, false)
	if flag is bool:
		return flag
	return false


## Depth-first search for a node entry by its accessible name.
func _find_by_name(entry: Dictionary, wanted: String) -> Dictionary:
	if _str_field(entry, "name") == wanted:
		return entry
	for child: Variant in _as_array(entry.get("children", [])):
		var found: Dictionary = _find_by_name(_as_dict(child), wanted)
		if not found.is_empty():
			return found
	return {}


func _tree() -> Dictionary:
	return ACCESSIBILITY_TREE.build(_root, 10)


func _role_of(node_name: String) -> String:
	return _str_field(_find_by_name(_tree(), node_name), "role")


# ── availability gate ────────────────────────────────────────────────────

func test_is_supported_matches_engine_version() -> void:
	var version: Dictionary = Engine.get_version_info()
	var major: int = version.get("major", 0)
	var minor: int = version.get("minor", 0)
	var expected: bool = major > 4 or (major == 4 and minor >= 5)
	assert_bool(ACCESSIBILITY_TREE.is_supported()).is_equal(expected)


func test_build_response_reports_source_as_derived() -> void:
	# The response must never claim to be the real AccessKit tree.
	var result: Dictionary = ACCESSIBILITY_TREE.build_response(_root, 10)
	assert_str(_str_field(result, "source")).is_equal("derived")


func test_build_response_on_unsupported_version_returns_clear_error() -> void:
	if ACCESSIBILITY_TREE.is_supported():
		return  # Supported engines are covered by the role tests below.
	var result: Dictionary = ACCESSIBILITY_TREE.build_response(_root, 10)
	assert_bool(result.has("error")).is_true()
	assert_str(_str_field(result, "error")).contains("4.5")


# ── role derivation ──────────────────────────────────────────────────────

func test_button_derives_button_role() -> void:
	if not ACCESSIBILITY_TREE.is_supported():
		return
	var btn: Button = auto_free(Button.new())
	_attach(btn, "Go")
	assert_str(_role_of("Go")).is_equal("button")


func test_check_box_derives_check_box_role() -> void:
	if not ACCESSIBILITY_TREE.is_supported():
		return
	var box: CheckBox = auto_free(CheckBox.new())
	_attach(box, "Toggle")
	assert_str(_role_of("Toggle")).is_equal("check_box")


func test_label_derives_static_text_role() -> void:
	if not ACCESSIBILITY_TREE.is_supported():
		return
	var label: Label = auto_free(Label.new())
	_attach(label, "Caption")
	assert_str(_role_of("Caption")).is_equal("static_text")


func test_line_edit_derives_text_field_role() -> void:
	if not ACCESSIBILITY_TREE.is_supported():
		return
	var field: LineEdit = auto_free(LineEdit.new())
	_attach(field, "NameInput")
	assert_str(_role_of("NameInput")).is_equal("text_field")


func test_text_edit_derives_multiline_text_field_role() -> void:
	if not ACCESSIBILITY_TREE.is_supported():
		return
	var editor: TextEdit = auto_free(TextEdit.new())
	_attach(editor, "Notes")
	assert_str(_role_of("Notes")).is_equal("multiline_text_field")


func test_non_control_node_derives_unknown_role() -> void:
	if not ACCESSIBILITY_TREE.is_supported():
		return
	var sprites: Node2D = auto_free(Node2D.new())
	_attach(sprites, "Sprites")
	assert_str(_role_of("Sprites")).is_equal("unknown")


func test_every_derivable_role_is_a_real_engine_role() -> void:
	# Guards against inventing role strings: every value the mapping can emit
	# must correspond to an actual DisplayServer.ROLE_* constant.
	if not ACCESSIBILITY_TREE.is_supported():
		return
	var valid: PackedStringArray = ACCESSIBILITY_TREE.engine_role_names()
	assert_int(valid.size()).is_greater(10)
	for role: String in ACCESSIBILITY_TREE.derivable_role_names():
		assert_bool(valid.has(role)).override_failure_message(
			"derived role '%s' is not a real DisplayServer.ROLE_* constant" % role
		).is_true()


# ── accessible name precedence ───────────────────────────────────────────

func test_author_set_accessibility_name_wins_over_text() -> void:
	var btn: Button = auto_free(Button.new())
	btn.text = "Visible Text"
	if btn.has_method("set_accessibility_name"):
		btn.call("set_accessibility_name", "Screen Reader Name")
	else:
		return  # Godot < 4.5 has no accessibility_name to prefer.
	_attach(btn, "Btn")
	assert_str(_str_field(_find_by_name(_tree(), "Screen Reader Name"), "name")).is_equal(
		"Screen Reader Name"
	)


func test_falls_back_to_visible_text() -> void:
	var btn: Button = auto_free(Button.new())
	btn.text = "Start Game"
	_attach(btn, "Btn")
	assert_str(_str_field(_find_by_name(_tree(), "Start Game"), "name")).is_equal("Start Game")


func test_falls_back_to_node_name_when_no_text() -> void:
	var btn: Button = auto_free(Button.new())
	_attach(btn, "NamelessBtn")
	assert_str(_str_field(_find_by_name(_tree(), "NamelessBtn"), "name")).is_equal("NamelessBtn")


# ── state ────────────────────────────────────────────────────────────────

func test_disabled_button_reports_disabled_state() -> void:
	var btn: Button = auto_free(Button.new())
	btn.disabled = true
	_attach(btn, "DisabledBtn")
	assert_bool(_state_flag(_find_by_name(_tree(), "DisabledBtn"), "disabled")).is_true()


func test_enabled_button_is_not_disabled() -> void:
	var btn: Button = auto_free(Button.new())
	_attach(btn, "EnabledBtn")
	assert_bool(_state_flag(_find_by_name(_tree(), "EnabledBtn"), "disabled")).is_false()


func test_hidden_control_reports_hidden_state() -> void:
	var btn: Button = auto_free(Button.new())
	btn.visible = false
	_attach(btn, "HiddenBtn")
	assert_bool(_state_flag(_find_by_name(_tree(), "HiddenBtn"), "hidden")).is_true()


func test_pressed_check_box_reports_checked_state() -> void:
	var box: CheckBox = auto_free(CheckBox.new())
	box.button_pressed = true
	_attach(box, "CheckedBox")
	assert_bool(_state_flag(_find_by_name(_tree(), "CheckedBox"), "checked")).is_true()


func test_plain_node_is_reported_as_not_focusable() -> void:
	var plain: Node = auto_free(Node.new())
	_attach(plain, "PlainNode")
	assert_bool(_state_flag(_find_by_name(_tree(), "PlainNode"), "focusable")).is_false()


# ── shape / traversal ────────────────────────────────────────────────────

func test_entry_carries_path_and_children() -> void:
	var btn: Button = auto_free(Button.new())
	_attach(btn, "Deep")
	var tree: Dictionary = _tree()
	assert_bool(tree.has("path")).is_true()
	assert_bool(tree.has("children")).is_true()
	assert_str(_str_field(_find_by_name(tree, "Deep"), "path")).contains("Deep")


func test_max_depth_limits_recursion() -> void:
	var mid: Control = auto_free(Control.new())
	_attach(mid, "Mid")
	var leaf: Button = auto_free(Button.new())
	leaf.name = "Leaf"
	mid.add_child(leaf)

	var shallow: Dictionary = ACCESSIBILITY_TREE.build(_root, 1)
	assert_dict(_find_by_name(shallow, "Leaf")).is_empty()

	var deep: Dictionary = ACCESSIBILITY_TREE.build(_root, 5)
	assert_dict(_find_by_name(deep, "Leaf")).is_not_empty()


func test_value_reflects_editable_text() -> void:
	var field: LineEdit = auto_free(LineEdit.new())
	field.text = "typed value"
	_attach(field, "Field")
	# The accessible name falls back to the text, so look the entry up by it.
	assert_str(_str_field(_find_by_name(_tree(), "typed value"), "value")).is_equal("typed value")


func test_build_response_counts_nodes() -> void:
	var btn: Button = auto_free(Button.new())
	_attach(btn, "Counted")
	var result: Dictionary = ACCESSIBILITY_TREE.build_response(_root, 10)
	if result.has("error"):
		return
	var count: int = result.get("count", 0)
	assert_int(count).is_equal(2)
