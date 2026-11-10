# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for StagehandSelfProtection and the handlers that enforce it: no
## selector-driven read, write, or call may reach Stagehand's own server node
## or its children (godot-stagehand-ext-review-2026-11-327t.1). A stand-in
## node joins the protected group so these run without a live server.

const PropertyTarget := preload("res://scripts/property_target.gd")
const SelfProtection := preload("res://addons/stagehand/core/self_protection.gd")
const Waiter := preload("res://addons/stagehand/core/waiter.gd")

const GUARDED: String = "group:self_protection_guarded"
const GUARDED_CHILD: String = "group:self_protection_guarded_child"
const OPEN: String = "group:self_protection_open"

var _guarded: Node
var _guarded_child: Node2D
var _open: Node


func before_test() -> void:
	_guarded = auto_free(Node.new())
	_guarded.name = "GuardedServer"
	_guarded.set_script(PropertyTarget)
	_guarded.add_to_group(SelfProtection.PROTECTED_GROUP)
	_guarded.add_to_group(&"self_protection_guarded")
	add_child(_guarded)

	_guarded_child = Node2D.new()
	_guarded_child.name = "GuardedChild"
	_guarded_child.add_to_group(&"self_protection_guarded_child")
	_guarded.add_child(_guarded_child)

	_open = auto_free(Node.new())
	_open.name = "OpenTarget"
	_open.set_script(PropertyTarget)
	_open.add_to_group(&"self_protection_open")
	add_child(_open)


func _assert_refused(result: Dictionary) -> void:
	assert_str(str(result.get("error_code", ""))).is_equal(StagehandErrors.NOT_SUPPORTED)
	var details: Dictionary = result.get("details", {})
	assert_str(str(details.get("next_action", ""))).is_not_empty()


# ── is_protected ─────────────────────────────────────────────────────────

func test_protected_node_and_descendants_are_protected() -> void:
	assert_bool(SelfProtection.is_protected(_guarded)).is_true()
	assert_bool(SelfProtection.is_protected(_guarded_child)).is_true()
	assert_bool(SelfProtection.is_protected(_open)).is_false()


func test_forbidden_property_names() -> void:
	assert_bool(SelfProtection.is_forbidden_property("_allow_unsafe")).is_true()
	assert_bool(SelfProtection.is_forbidden_property("script")).is_true()
	assert_bool(SelfProtection.is_forbidden_property("script.resource_path")).is_true()
	assert_bool(SelfProtection.is_forbidden_property("position._x")).is_true()
	assert_bool(SelfProtection.is_forbidden_property("position:_x")).is_true()
	assert_bool(SelfProtection.is_forbidden_property("position.x")).is_false()
	assert_bool(SelfProtection.is_forbidden_property("flag_prop")).is_false()
	assert_bool(SelfProtection.is_forbidden_property("scripted")).is_false()


# ── get_property / set_property ──────────────────────────────────────────

func test_get_property_on_protected_node_is_refused() -> void:
	_assert_refused(StagehandPropertyHandler.get_property(
		get_tree(), {"selector": GUARDED, "property": "flag_prop"}
	))


func test_set_property_on_protected_node_is_refused() -> void:
	_assert_refused(StagehandPropertyHandler.set_property(
		get_tree(), {"selector": GUARDED, "property": "flag_prop", "value": false}
	))
	assert_bool(_guarded.get("flag_prop")).is_true()


func test_set_property_on_protected_descendant_is_refused() -> void:
	_assert_refused(StagehandPropertyHandler.set_property(
		get_tree(), {"selector": GUARDED_CHILD, "property": "visible", "value": false}
	))
	assert_bool(_guarded_child.visible).is_true()


func test_set_property_on_open_node_still_works() -> void:
	var result: Dictionary = StagehandPropertyHandler.set_property(
		get_tree(), {"selector": OPEN, "property": "flag_prop", "value": false}
	)
	assert_bool(result.get("success", false)).is_true()


# ── call_method ──────────────────────────────────────────────────────────

func test_call_method_on_protected_node_is_refused() -> void:
	_assert_refused(StagehandMethodHandler.call_method(
		get_tree(), {"selector": GUARDED, "method": "get_instance_id"}
	))


func test_call_method_allow_multiple_including_protected_node_is_refused() -> void:
	_assert_refused(StagehandMethodHandler.call_method(get_tree(), {
		"selector": GUARDED_CHILD,
		"method": "get_instance_id",
		"allow_multiple": true,
	}))


# ── query_nodes / get_tree ───────────────────────────────────────────────

func test_query_nodes_with_properties_on_protected_node_is_refused() -> void:
	var properties: Array[String] = ["flag_prop"]
	_assert_refused(StagehandTreeSerializer.query_nodes(get_tree(), GUARDED, properties))


func test_query_nodes_without_properties_lists_protected_node() -> void:
	var result: Dictionary = StagehandTreeSerializer.query_nodes(get_tree(), GUARDED)
	assert_bool(result.has("error")).is_false()
	assert_int(result.get("count", 0)).is_equal(1)


func test_serialize_tree_omits_properties_of_protected_nodes() -> void:
	var properties: Array[String] = ["flag_prop"]
	var data: Dictionary = StagehandTreeSerializer.serialize_tree(_guarded, 1, properties)
	assert_bool(data.has("properties")).is_false()
	var children: Array = data.get("children", [])
	assert_int(children.size()).is_equal(1)
	var child: Dictionary = children[0]
	assert_bool(child.has("properties")).is_false()

	var open_data: Dictionary = StagehandTreeSerializer.serialize_tree(_open, 0, properties)
	assert_bool(open_data.has("properties")).is_true()


# ── wait_for_property ────────────────────────────────────────────────────

func test_wait_for_property_on_protected_node_is_refused() -> void:
	var waiter: Waiter = auto_free(Waiter.new())
	add_child(waiter)
	var result: Dictionary = await waiter.wait_for_property(
		GUARDED, "flag_prop", "==", true, 100, 20
	)
	_assert_refused(result)
