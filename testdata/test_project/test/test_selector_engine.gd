# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for SelectorEngine — selector parsing, resolution of every selector
## type, `>>` chaining, and the interactive-control-first ranking used to
## disambiguate clicks (godot-stagehand-phase3-vrj.20).
##
## Resolution tests build a fixture subtree under this suite node, which the
## GdUnit4 runner adds to the live scene tree. Selectors resolve from
## `tree.root`, so every fixture name, group, and text value is deliberately
## distinctive to avoid colliding with the runner's own nodes.

## Distinctive infix shared by fixture node names so `name:` globs can target
## the fixture subtree without matching runner-owned nodes.
const FIXTURE_TAG: String = "SelFixture"

var _root: Node
var _panel: Control
var _button: Button
var _label: Label


func before_test() -> void:
	_root = auto_free(Node.new())
	_root.name = "%sRoot" % FIXTURE_TAG
	add_child(_root)

	_panel = auto_free(Control.new())
	_panel.name = "%sPanel" % FIXTURE_TAG
	_panel.add_to_group(&"sel_fixture_panels")
	_root.add_child(_panel)

	_button = auto_free(Button.new())
	_button.name = "%sButton" % FIXTURE_TAG
	_button.text = "Launch Sequence"
	_button.add_to_group(&"sel_fixture_widgets")
	_button.set_meta("sel_fixture_role", "primary")
	_panel.add_child(_button)

	_label = auto_free(Label.new())
	_label.name = "%sLabel" % FIXTURE_TAG
	_label.text = "Launch Sequence readout"
	_label.add_to_group(&"sel_fixture_widgets")
	_panel.add_child(_label)


func _query(selector: String) -> Array[Node]:
	return SelectorEngine.query(get_tree(), selector)


# ── parse() ──────────────────────────────────────────────────────────────

func test_parse_path_has_no_prefix() -> void:
	var parsed: Dictionary = SelectorEngine.parse("/root/UI/Button")
	assert_int(parsed["type"]).is_equal(SelectorEngine.SelectorType.PATH)
	assert_str(parsed["value"]).is_equal("/root/UI/Button")


func test_parse_name() -> void:
	var parsed: Dictionary = SelectorEngine.parse("name:*Button*")
	assert_int(parsed["type"]).is_equal(SelectorEngine.SelectorType.NAME)
	assert_str(parsed["value"]).is_equal("*Button*")


func test_parse_class() -> void:
	var parsed: Dictionary = SelectorEngine.parse("class:Button")
	assert_int(parsed["type"]).is_equal(SelectorEngine.SelectorType.CLASS)
	assert_str(parsed["value"]).is_equal("Button")


func test_parse_group() -> void:
	var parsed: Dictionary = SelectorEngine.parse("group:interactive")
	assert_int(parsed["type"]).is_equal(SelectorEngine.SelectorType.GROUP)
	assert_str(parsed["value"]).is_equal("interactive")


func test_parse_meta() -> void:
	var parsed: Dictionary = SelectorEngine.parse("meta:role=primary")
	assert_int(parsed["type"]).is_equal(SelectorEngine.SelectorType.META)
	assert_str(parsed["value"]).is_equal("role=primary")


func test_parse_unique() -> void:
	var parsed: Dictionary = SelectorEngine.parse("unique:SaveButton")
	assert_int(parsed["type"]).is_equal(SelectorEngine.SelectorType.UNIQUE)
	assert_str(parsed["value"]).is_equal("SaveButton")


func test_parse_text_loose() -> void:
	var parsed: Dictionary = SelectorEngine.parse("text:CONTINUE")
	assert_int(parsed["type"]).is_equal(SelectorEngine.SelectorType.TEXT)
	assert_str(parsed["value"]).is_equal("CONTINUE")


func test_parse_text_exact() -> void:
	var parsed: Dictionary = SelectorEngine.parse("text=CONTINUE")
	assert_int(parsed["type"]).is_equal(SelectorEngine.SelectorType.TEXT_EXACT)
	assert_str(parsed["value"]).is_equal("CONTINUE")


func test_parse_strips_surrounding_whitespace() -> void:
	var parsed: Dictionary = SelectorEngine.parse("   class:Button   ")
	assert_int(parsed["type"]).is_equal(SelectorEngine.SelectorType.CLASS)
	assert_str(parsed["value"]).is_equal("Button")


# ── parse() edge cases ───────────────────────────────────────────────────

func test_parse_empty_string_is_invalid() -> void:
	assert_bool(SelectorEngine.parse("").is_empty()).is_true()


func test_parse_whitespace_only_is_invalid() -> void:
	assert_bool(SelectorEngine.parse("    ").is_empty()).is_true()


func test_parse_text_exact_empty_value_is_invalid() -> void:
	assert_bool(SelectorEngine.parse("text=").is_empty()).is_true()


func test_parse_prefix_with_empty_value_is_invalid() -> void:
	# Every recognized prefix must reject an empty operand rather than
	# resolving to "match everything".
	for selector: String in ["name:", "class:", "group:", "text:", "meta:", "unique:"]:
		assert_bool(SelectorEngine.parse(selector).is_empty()) \
			.override_failure_message("expected '%s' to be invalid" % selector) \
			.is_true()


func test_parse_unrecognized_prefix_falls_back_to_path() -> void:
	# "bogus:" is not a known prefix, so it is treated as a node path — which
	# simply resolves to no nodes rather than erroring at parse time.
	var parsed: Dictionary = SelectorEngine.parse("bogus:Thing")
	assert_int(parsed["type"]).is_equal(SelectorEngine.SelectorType.PATH)
	assert_int(_query("bogus:Thing").size()).is_equal(0)


# ── query() resolution per selector type ─────────────────────────────────

func test_query_path_resolves_single_node() -> void:
	var results: Array[Node] = _query(str(_button.get_path()))
	assert_int(results.size()).is_equal(1)
	assert_object(results[0]).is_same(_button)


func test_query_path_missing_node_returns_empty() -> void:
	assert_int(_query("/root/NoSuchNodeAnywhere").size()).is_equal(0)


func test_query_name_glob_matches_fixture_subtree() -> void:
	var results: Array[Node] = _query("name:%s*" % FIXTURE_TAG)
	assert_int(results.size()).is_equal(4)


func test_query_name_exact_matches_one_node() -> void:
	var results: Array[Node] = _query("name:%sButton" % FIXTURE_TAG)
	assert_int(results.size()).is_equal(1)
	assert_object(results[0]).is_same(_button)


func test_query_name_no_match_returns_empty() -> void:
	assert_int(_query("name:NoSuchNodeName*").size()).is_equal(0)


func test_query_class_matches_subclass_instances() -> void:
	var results: Array[Node] = _query("class:Button")
	assert_array(results).contains([_button])
	assert_array(results).not_contains([_label])


func test_query_class_unknown_class_returns_empty() -> void:
	assert_int(_query("class:NotARealGodotClass").size()).is_equal(0)


func test_query_group_returns_all_members() -> void:
	var results: Array[Node] = _query("group:sel_fixture_widgets")
	assert_int(results.size()).is_equal(2)
	assert_array(results).contains([_button, _label])


func test_query_group_unknown_group_returns_empty() -> void:
	assert_int(_query("group:no_such_group_exists").size()).is_equal(0)


func test_query_text_loose_matches_substring_case_insensitively() -> void:
	var results: Array[Node] = _query("text:launch sequence")
	assert_array(results).contains([_button, _label])


func test_query_text_exact_excludes_partial_matches() -> void:
	# The Label's text merely *contains* the phrase; only the Button equals it.
	var results: Array[Node] = _query("text=Launch Sequence")
	assert_array(results).contains([_button])
	assert_array(results).not_contains([_label])


func test_query_text_no_match_returns_empty() -> void:
	assert_int(_query("text:no node has this text at all").size()).is_equal(0)


func test_query_meta_key_and_value() -> void:
	var results: Array[Node] = _query("meta:sel_fixture_role=primary")
	assert_int(results.size()).is_equal(1)
	assert_object(results[0]).is_same(_button)


func test_query_meta_key_only_ignores_value() -> void:
	var results: Array[Node] = _query("meta:sel_fixture_role")
	assert_int(results.size()).is_equal(1)
	assert_object(results[0]).is_same(_button)


func test_query_meta_wrong_value_returns_empty() -> void:
	assert_int(_query("meta:sel_fixture_role=secondary").size()).is_equal(0)


func test_query_unique_matches_node_name() -> void:
	var results: Array[Node] = _query("unique:%sButton" % FIXTURE_TAG)
	assert_array(results).contains([_button])


func test_query_unique_no_match_returns_empty() -> void:
	assert_int(_query("unique:NothingNamedLikeThis").size()).is_equal(0)


func test_query_empty_selector_returns_empty() -> void:
	assert_int(_query("").size()).is_equal(0)


# ── query() chaining with >> ─────────────────────────────────────────────

func test_chain_scopes_second_selector_to_first_match() -> void:
	var results: Array[Node] = _query("name:%sPanel >> class:Button" % FIXTURE_TAG)
	assert_int(results.size()).is_equal(1)
	assert_object(results[0]).is_same(_button)


func test_chain_three_deep() -> void:
	var results: Array[Node] = _query(
		"name:%sRoot >> name:%sPanel >> name:%sLabel" % [FIXTURE_TAG, FIXTURE_TAG, FIXTURE_TAG]
	)
	assert_int(results.size()).is_equal(1)
	assert_object(results[0]).is_same(_label)


func test_chain_group_scoped_to_ancestor() -> void:
	var results: Array[Node] = _query("name:%sPanel >> group:sel_fixture_widgets" % FIXTURE_TAG)
	assert_int(results.size()).is_equal(2)


func test_chain_excludes_the_scope_node_itself() -> void:
	# The Panel is in sel_fixture_panels, but scoping to itself must not
	# return itself — chaining descends strictly into children.
	var results: Array[Node] = _query("name:%sPanel >> group:sel_fixture_panels" % FIXTURE_TAG)
	assert_int(results.size()).is_equal(0)


func test_chain_with_no_match_in_second_part_returns_empty() -> void:
	assert_int(_query("name:%sPanel >> class:ProgressBar" % FIXTURE_TAG).size()).is_equal(0)


func test_chain_with_unmatched_first_part_returns_empty() -> void:
	assert_int(_query("name:NoSuchAncestor >> class:Button").size()).is_equal(0)


func test_chain_with_empty_part_returns_empty() -> void:
	assert_int(_query("name:%sPanel >> " % FIXTURE_TAG).size()).is_equal(0)


func test_chain_with_invalid_part_returns_empty() -> void:
	# "class:" has an empty operand, so the whole chain fails to parse.
	assert_int(_query("name:%sPanel >> class:" % FIXTURE_TAG).size()).is_equal(0)


func test_chain_rejects_absolute_path_in_later_part() -> void:
	# An absolute path can't be scoped under a parent, so it matches nothing.
	assert_int(_query("name:%sPanel >> /root" % FIXTURE_TAG).size()).is_equal(0)


# ── rank_for_interaction() ───────────────────────────────────────────────

func test_rank_prefers_button_over_label() -> void:
	# Mirrors the water-wars bug: a Label and a Button both match "Continue".
	var label: Label = auto_free(Label.new())
	label.text = "Continue from day 8, 13:00?"
	var button: Button = auto_free(Button.new())
	button.text = "CONTINUE"

	# Label appears first in tree order; ranking must still pick the Button.
	var nodes: Array[Node] = [label, button]
	var ranked: Array[Node] = SelectorEngine.rank_for_interaction(nodes)

	assert_object(ranked[0]).is_same(button)
	assert_object(ranked[1]).is_same(label)


func test_rank_is_stable_within_tier() -> void:
	var first: Label = auto_free(Label.new())
	var second: Label = auto_free(Label.new())
	var nodes: Array[Node] = [first, second]
	var ranked: Array[Node] = SelectorEngine.rank_for_interaction(nodes)

	# No interactive control present: original order preserved.
	assert_object(ranked[0]).is_same(first)
	assert_object(ranked[1]).is_same(second)


func test_rank_demotes_ignore_filter_control() -> void:
	# A Control with MOUSE_FILTER_IGNORE should rank below one that receives input.
	var ignored: Control = auto_free(Control.new())
	ignored.mouse_filter = Control.MOUSE_FILTER_IGNORE
	var clickable: Control = auto_free(Control.new())
	clickable.mouse_filter = Control.MOUSE_FILTER_STOP

	var nodes: Array[Node] = [ignored, clickable]
	var ranked: Array[Node] = SelectorEngine.rank_for_interaction(nodes)

	assert_object(ranked[0]).is_same(clickable)
	assert_object(ranked[1]).is_same(ignored)


func test_rank_empty_input_returns_empty() -> void:
	var nodes: Array[Node] = []
	assert_int(SelectorEngine.rank_for_interaction(nodes).size()).is_equal(0)
