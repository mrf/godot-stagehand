# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for StagehandTreeSerializer — scene-tree snapshots, depth limiting,
## property inclusion, and JSON-safe value conversion.

var _root: Node2D
var _child: Node
var _grandchild: Node


func before_test() -> void:
	# Root
	#  └─ Child
	#      └─ Grandchild
	_root = auto_free(Node2D.new())
	_root.name = "SerRoot"
	_root.position = Vector2(10.0, 20.0)
	add_child(_root)

	_child = auto_free(Node.new())
	_child.name = "SerChild"
	_root.add_child(_child)

	_grandchild = auto_free(Node.new())
	_grandchild.name = "SerGrandchild"
	_child.add_child(_grandchild)


func _children_of(data: Dictionary) -> Array:
	return data.get("children", [])


## The serialized match list from a query_nodes result.
func _nodes_of(result: Dictionary) -> Array:
	return result.get("nodes", [])


# ── serialize_tree: shape ────────────────────────────────────────────────

func test_serialize_single_node_reports_identity_fields() -> void:
	var leaf: Node = auto_free(Node.new())
	leaf.name = "Lonely"
	var data: Dictionary = StagehandTreeSerializer.serialize_tree(leaf)

	assert_str(str(data.get("name"))).is_equal("Lonely")
	assert_str(str(data.get("class"))).is_equal("Node")
	assert_bool(data.has("path")).is_true()


func test_serialize_single_node_counts_itself() -> void:
	var leaf: Node = auto_free(Node.new())
	assert_int(StagehandTreeSerializer.serialize_tree(leaf).get("count", -1)).is_equal(1)


func test_serialize_tree_counts_every_descendant() -> void:
	# Root + Child + Grandchild.
	assert_int(StagehandTreeSerializer.serialize_tree(_root).get("count", -1)).is_equal(3)


func test_serialize_tree_nests_children() -> void:
	var data: Dictionary = StagehandTreeSerializer.serialize_tree(_root)

	var children: Array = _children_of(data)
	assert_int(children.size()).is_equal(1)

	var child: Dictionary = children[0]
	assert_str(str(child.get("name"))).is_equal("SerChild")

	var grandchildren: Array = _children_of(child)
	assert_int(grandchildren.size()).is_equal(1)
	var grandchild: Dictionary = grandchildren[0]
	assert_str(str(grandchild.get("name"))).is_equal("SerGrandchild")


func test_serialize_deep_tree_reaches_the_deepest_node() -> void:
	# A 20-node chain, deeper than serialize_tree's default max_depth of 10.
	var chain_root: Node = auto_free(Node.new())
	chain_root.name = "Chain0"
	var cursor: Node = chain_root
	for i: int in range(1, 20):
		var next_node: Node = Node.new()
		next_node.name = "Chain%d" % i
		cursor.add_child(next_node)
		cursor = next_node

	var data: Dictionary = StagehandTreeSerializer.serialize_tree(chain_root, 20)

	# Walk down 19 levels and confirm the last link is present.
	var node_data: Dictionary = data
	for i: int in range(1, 20):
		var children: Array = _children_of(node_data)
		assert_int(children.size()) \
			.override_failure_message("chain broke at depth %d" % i) \
			.is_equal(1)
		node_data = children[0]
	assert_str(str(node_data.get("name"))).is_equal("Chain19")
	assert_bool(_children_of(node_data).is_empty()).is_true()


# ── serialize_tree: max_depth cutoff ─────────────────────────────────────

func test_max_depth_zero_omits_children_entirely() -> void:
	var data: Dictionary = StagehandTreeSerializer.serialize_tree(_root, 0)
	assert_bool(data.has("children")).is_false()
	# count is a whole-subtree tally and is not affected by the depth cutoff.
	assert_int(data.get("count", -1)).is_equal(3)


func test_max_depth_one_stops_below_direct_children() -> void:
	var data: Dictionary = StagehandTreeSerializer.serialize_tree(_root, 1)
	var children: Array = _children_of(data)
	assert_int(children.size()).is_equal(1)

	var child: Dictionary = children[0]
	assert_str(str(child.get("name"))).is_equal("SerChild")
	# Depth budget spent: the grandchild level is cut off.
	assert_bool(child.has("children")).is_false()


func test_max_depth_two_includes_grandchildren() -> void:
	var data: Dictionary = StagehandTreeSerializer.serialize_tree(_root, 2)
	var child: Dictionary = _children_of(data)[0]
	assert_int(_children_of(child).size()).is_equal(1)


func test_negative_max_depth_behaves_like_zero() -> void:
	var data: Dictionary = StagehandTreeSerializer.serialize_tree(_root, -5)
	assert_bool(data.has("children")).is_false()


# ── property inclusion ───────────────────────────────────────────────────

func test_no_properties_requested_omits_properties_key() -> void:
	var data: Dictionary = StagehandTreeSerializer.serialize_tree(_root)
	assert_bool(data.has("properties")).is_false()


func test_requested_property_is_included_and_json_safe() -> void:
	var data: Dictionary = StagehandTreeSerializer.serialize_tree(_root, 10, ["position"])
	var props: Dictionary = data.get("properties", {})

	# Vector2 must be converted to a JSON object, not left as a Godot type.
	var position: Dictionary = props.get("position", {})
	assert_float(position.get("x", 0.0)).is_equal_approx(10.0, 0.001)
	assert_float(position.get("y", 0.0)).is_equal_approx(20.0, 0.001)


func test_dot_notation_property_is_supported() -> void:
	var data: Dictionary = StagehandTreeSerializer.serialize_tree(_root, 10, ["position.x"])
	var props: Dictionary = data.get("properties", {})
	assert_float(props.get("position.x", 0.0)).is_equal_approx(10.0, 0.001)


func test_properties_are_included_on_descendants_too() -> void:
	var data: Dictionary = StagehandTreeSerializer.serialize_tree(_root, 10, ["name"])
	var child: Dictionary = _children_of(data)[0]
	assert_bool(child.has("properties")).is_true()


func test_unknown_property_is_silently_omitted() -> void:
	var data: Dictionary = StagehandTreeSerializer.serialize_tree(
		_root, 10, ["no_such_property_at_all"]
	)
	var props: Dictionary = data.get("properties", {})
	assert_bool(props.has("no_such_property_at_all")).is_false()


func test_unknown_property_does_not_drop_valid_siblings() -> void:
	var data: Dictionary = StagehandTreeSerializer.serialize_tree(
		_root, 10, ["no_such_property_at_all", "position"]
	)
	var props: Dictionary = data.get("properties", {})
	assert_bool(props.has("position")).is_true()


# ── query_nodes ──────────────────────────────────────────────────────────

func test_query_nodes_returns_matches_and_total_count() -> void:
	_child.add_to_group(&"ser_query_group")
	_grandchild.add_to_group(&"ser_query_group")

	var result: Dictionary = StagehandTreeSerializer.query_nodes(
		get_tree(), "group:ser_query_group"
	)
	assert_int(result.get("count", -1)).is_equal(2)
	assert_int(_nodes_of(result).size()).is_equal(2)


func test_query_nodes_limit_caps_results_but_not_count() -> void:
	_root.add_to_group(&"ser_limit_group")
	_child.add_to_group(&"ser_limit_group")
	_grandchild.add_to_group(&"ser_limit_group")

	var result: Dictionary = StagehandTreeSerializer.query_nodes(
		get_tree(), "group:ser_limit_group", [], 2
	)
	# count reports every match; nodes is truncated to the limit.
	assert_int(result.get("count", -1)).is_equal(3)
	assert_int(_nodes_of(result).size()).is_equal(2)


func test_query_nodes_results_are_flat_not_nested() -> void:
	_child.add_to_group(&"ser_flat_group")
	var result: Dictionary = StagehandTreeSerializer.query_nodes(
		get_tree(), "group:ser_flat_group"
	)
	var first: Dictionary = _nodes_of(result)[0]
	assert_bool(first.has("children")).is_false()


func test_query_nodes_no_match_returns_empty() -> void:
	var result: Dictionary = StagehandTreeSerializer.query_nodes(
		get_tree(), "group:no_such_group_at_all"
	)
	assert_int(result.get("count", -1)).is_equal(0)
	assert_bool(_nodes_of(result).is_empty()).is_true()


func test_query_nodes_invalid_selector_returns_empty() -> void:
	var result: Dictionary = StagehandTreeSerializer.query_nodes(get_tree(), "")
	assert_int(result.get("count", -1)).is_equal(0)
