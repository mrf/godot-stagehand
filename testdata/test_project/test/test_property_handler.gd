# GdUnit4 assertions are fluent and return self for chaining, so every
# unchained assert_*() trips return_value_discarded=2. Scoped, deliberate
# relaxation of that one warning; all other strict warnings stay errors.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for StagehandPropertyHandler — get/set via selector, dot notation,
## JSON→Godot value coercion, and the read-back success contract
## (godot-stagehand-jzs: a guarded setter must not report success).

const PropertyTarget := preload("res://scripts/property_target.gd")

const GROUP: StringName = &"prop_handler_target"

var _node: Node2D
var _target: Node


func before_test() -> void:
	_node = auto_free(Node2D.new())
	_node.name = "PropNode"
	_node.position = Vector2(3.0, 4.0)
	_node.add_to_group(GROUP)
	add_child(_node)

	# Script-backed node exposing one property per affected Variant type.
	_target = auto_free(Node.new())
	_target.name = "PropTarget"
	_target.set_script(PropertyTarget)
	_target.add_to_group(&"prop_handler_script_target")
	add_child(_target)


func _get_prop(selector: String, property: String) -> Dictionary:
	return StagehandPropertyHandler.get_property(
		get_tree(), {"selector": selector, "property": property}
	)


func _set_prop(selector: String, property: String, value: Variant) -> Dictionary:
	return StagehandPropertyHandler.set_property(
		get_tree(), {"selector": selector, "property": property, "value": value}
	)


func _get_on_target(property: String) -> Dictionary:
	return _get_prop("group:prop_handler_script_target", property)


func _set_on_target(property: String, value: Variant) -> Dictionary:
	return _set_prop("group:prop_handler_script_target", property, value)


# ── get_property: happy path ─────────────────────────────────────────────

func test_get_simple_string_property() -> void:
	var result: Dictionary = _get_prop("group:%s" % GROUP, "name")
	assert_str(str(result.get("value"))).is_equal("PropNode")
	# Node.name is a StringName, not a String — the reported type is the
	# property's real Variant type, not a normalized "string".
	assert_str(str(result.get("type"))).is_equal("StringName")


func test_get_vector_property_is_json_safe() -> void:
	var result: Dictionary = _get_prop("group:%s" % GROUP, "position")
	var value: Dictionary = result.get("value", {})
	assert_float(value.get("x", 0.0)).is_equal_approx(3.0, 0.001)
	assert_float(value.get("y", 0.0)).is_equal_approx(4.0, 0.001)
	assert_str(str(result.get("type"))).is_equal("Vector2")


func test_get_property_dot_notation() -> void:
	var result: Dictionary = _get_prop("group:%s" % GROUP, "position.x")
	assert_float(result.get("value", 0.0)).is_equal_approx(3.0, 0.001)
	assert_str(str(result.get("type"))).is_equal("float")


func test_get_script_declared_property() -> void:
	assert_int(_get_on_target("count_prop").get("value", -1)).is_equal(5)


# ── get_property: error cases ────────────────────────────────────────────

func test_get_missing_selector_returns_error() -> void:
	var result: Dictionary = StagehandPropertyHandler.get_property(
		get_tree(), {"property": "name"}
	)
	assert_str(str(result.get("error", ""))).contains("Missing selector")


func test_get_missing_property_returns_error() -> void:
	var result: Dictionary = StagehandPropertyHandler.get_property(
		get_tree(), {"selector": "group:%s" % GROUP}
	)
	assert_str(str(result.get("error", ""))).contains("Missing selector")


func test_get_unmatched_selector_returns_node_not_found() -> void:
	var result: Dictionary = _get_prop("group:no_such_group_at_all", "name")
	assert_str(str(result.get("error", ""))).contains("Node not found")
	assert_bool(result.has("value")).is_false()


func test_get_unknown_property_returns_property_not_found() -> void:
	var result: Dictionary = _get_prop("group:%s" % GROUP, "no_such_property_at_all")
	assert_str(str(result.get("error", ""))).contains("Property not found")


# ── set_property: happy path ─────────────────────────────────────────────

func test_set_simple_property_and_report_previous() -> void:
	var result: Dictionary = _set_on_target("text_prop", "updated")
	assert_bool(result.get("success", false)).is_true()
	assert_str(str(result.get("previous_value"))).is_equal("initial")
	assert_str(str(_target.get("text_prop"))).is_equal("updated")


func test_set_int_property() -> void:
	assert_bool(_set_on_target("count_prop", 42).get("success", false)).is_true()
	assert_int(_target.get("count_prop")).is_equal(42)


func test_set_bool_property_to_false() -> void:
	assert_bool(_set_on_target("flag_prop", false).get("success", false)).is_true()
	assert_bool(_target.get("flag_prop")).is_false()


## JSON has no boolean-vs-string distinction for some clients, so a bool
## property given the string "false" must coerce rather than reject.
func test_set_bool_property_from_string() -> void:
	assert_bool(_set_on_target("string_bool_prop", "false").get("success", false)).is_true()
	assert_bool(_target.get("string_bool_prop")).is_false()


## JSON has no Vector type; an {x, y} object must coerce to Vector2.
func test_set_vector2_property_from_json_object() -> void:
	var result: Dictionary = _set_on_target("vector2_prop", {"x": 7.0, "y": 8.0})
	assert_bool(result.get("success", false)).is_true()
	assert_vector(_target.get("vector2_prop")).is_equal(Vector2(7.0, 8.0))


func test_set_color_property_from_json_object() -> void:
	var result: Dictionary = _set_on_target(
		"color_prop", {"r": 0.0, "g": 0.5, "b": 1.0, "a": 1.0}
	)
	assert_bool(result.get("success", false)).is_true()


func test_set_property_dot_notation() -> void:
	var result: Dictionary = _set_prop("group:%s" % GROUP, "position.x", 99.0)
	assert_bool(result.get("success", false)).is_true()
	assert_float(_node.position.x).is_equal_approx(99.0, 0.001)
	# The untouched component must survive the nested write.
	assert_float(_node.position.y).is_equal_approx(4.0, 0.001)


# ── set_property: error cases ────────────────────────────────────────────

func test_set_missing_selector_returns_error() -> void:
	var result: Dictionary = StagehandPropertyHandler.set_property(
		get_tree(), {"property": "name", "value": "x"}
	)
	assert_str(str(result.get("error", ""))).contains("Missing selector")


func test_set_unmatched_selector_returns_node_not_found() -> void:
	var result: Dictionary = _set_prop("group:no_such_group_at_all", "name", "x")
	assert_str(str(result.get("error", ""))).contains("Node not found")


func test_set_unknown_property_reports_failure() -> void:
	# set_indexed silently no-ops on an unknown property, so the read-back
	# check is what catches it: success is false (there is no error string —
	# the contract is the success flag).
	var result: Dictionary = _set_prop("group:%s" % GROUP, "no_such_property_at_all", 1)
	assert_bool(result.get("success", false)).is_false()


func test_set_vector2_from_uncoercible_value_returns_error() -> void:
	var result: Dictionary = _set_on_target("vector2_prop", "not a vector")
	assert_str(str(result.get("error", ""))).contains("cannot be converted")


func test_set_int_from_fractional_float_returns_error() -> void:
	# 1.5 has no lossless int representation, so it must be rejected rather
	# than silently truncated to 1.
	var result: Dictionary = _set_on_target("count_prop", 1.5)
	# The int coercion path reports the generic "invalid value" message rather
	# than the type-specific one used by the Vector/Color coercions.
	assert_str(str(result.get("error", ""))).contains("count_prop")
	assert_int(_target.get("count_prop")).is_equal(5)


## godot-stagehand-jzs: a guarded setter can leave the value unchanged even
## though the property exists and Object.set() raised no error. success must
## reflect the post-set read-back, not merely "the property was found".
func test_set_rejected_by_guarded_setter_reports_failure() -> void:
	var result: Dictionary = _set_on_target("guarded_flag", false)
	assert_bool(result.get("success", true)).is_false()
	assert_bool(_target.get("guarded_flag")).is_true()
