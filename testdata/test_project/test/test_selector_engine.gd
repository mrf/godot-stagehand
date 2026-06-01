extends GdUnitTestSuite
## Tests for SelectorEngine — selector parsing and interaction ranking.
##
## Covers godot-stagehand-phase3-vrj.20: text= exact matching and the
## interactive-control-first ranking used to disambiguate clicks.

const SelectorEngine := preload("res://addons/stagehand/core/selector_engine.gd")


func test_parse_text_loose() -> void:
	var parsed: Dictionary = SelectorEngine.parse("text:CONTINUE")
	assert_int(parsed["type"]).is_equal(SelectorEngine.SelectorType.TEXT)
	assert_str(parsed["value"]).is_equal("CONTINUE")


func test_parse_text_exact() -> void:
	var parsed: Dictionary = SelectorEngine.parse("text=CONTINUE")
	assert_int(parsed["type"]).is_equal(SelectorEngine.SelectorType.TEXT_EXACT)
	assert_str(parsed["value"]).is_equal("CONTINUE")


func test_parse_text_exact_empty_value_is_invalid() -> void:
	var parsed: Dictionary = SelectorEngine.parse("text=")
	assert_bool(parsed.is_empty()).is_true()


func test_rank_prefers_button_over_label() -> void:
	# Mirrors the water-wars bug: a Label and a Button both match "Continue".
	var label: Label = auto_free(Label.new())
	label.text = "Continue from day 8, 13:00?"
	var button: Button = auto_free(Button.new())
	button.text = "CONTINUE"

	# Label appears first in tree order; ranking must still pick the Button.
	var nodes: Array[Node] = [label, button]
	var ranked: Array[Node] = SelectorEngine.rank_for_interaction(nodes)

	assert_that(ranked[0]).is_same(button)
	assert_that(ranked[1]).is_same(label)


func test_rank_is_stable_within_tier() -> void:
	var first: Label = auto_free(Label.new())
	var second: Label = auto_free(Label.new())
	var nodes: Array[Node] = [first, second]
	var ranked: Array[Node] = SelectorEngine.rank_for_interaction(nodes)

	# No interactive control present: original order preserved.
	assert_that(ranked[0]).is_same(first)
	assert_that(ranked[1]).is_same(second)


func test_rank_demotes_ignore_filter_control() -> void:
	# A Control with MOUSE_FILTER_IGNORE should rank below one that receives input.
	var ignored: Control = auto_free(Control.new())
	ignored.mouse_filter = Control.MOUSE_FILTER_IGNORE
	var clickable: Control = auto_free(Control.new())
	clickable.mouse_filter = Control.MOUSE_FILTER_STOP

	var nodes: Array[Node] = [ignored, clickable]
	var ranked: Array[Node] = SelectorEngine.rank_for_interaction(nodes)

	assert_that(ranked[0]).is_same(clickable)
	assert_that(ranked[1]).is_same(ignored)
