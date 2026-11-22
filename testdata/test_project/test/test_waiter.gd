# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for the Waiter node — condition polling, node-state waits, property
## conditions, signal waits, and timeout behavior.
##
## Timeouts are kept short (tens of ms) so the suite stays fast; each timeout
## assertion checks the returned contract rather than wall-clock precision,
## which is not reliable enough to assert on tightly.

const Waiter := preload("res://addons/stagehand/core/waiter.gd")
const SignalEmitter := preload("res://test/fixtures/signal_emitter.gd")

## Long enough to poll a few times, short enough to keep the suite quick.
const TIMEOUT_MS: int = 120
const POLL_MS: int = 20

var _waiter: Waiter
var _probe: Node


func before_test() -> void:
	_waiter = auto_free(Waiter.new())
	_waiter.name = "WaiterUnderTest"
	add_child(_waiter)

	_probe = auto_free(Node.new())
	_probe.name = "WaitProbe"
	_probe.add_to_group(&"waiter_probe")
	add_child(_probe)


# ── wait_condition ───────────────────────────────────────────────────────

func test_condition_already_true_returns_immediately() -> void:
	var met: bool = await _waiter.wait_condition(
		func() -> bool: return true, TIMEOUT_MS, POLL_MS
	)
	assert_bool(met).is_true()


func test_condition_never_true_times_out_false() -> void:
	var met: bool = await _waiter.wait_condition(
		func() -> bool: return false, TIMEOUT_MS, POLL_MS
	)
	assert_bool(met).is_false()


func test_condition_becoming_true_midway_is_detected() -> void:
	var polls: Array[int] = [0]
	var met: bool = await _waiter.wait_condition(
		func() -> bool:
			polls[0] += 1
			return polls[0] >= 3,
		1000,
		POLL_MS
	)
	assert_bool(met).is_true()
	assert_int(polls[0]).is_greater_equal(3)


func test_zero_timeout_still_evaluates_the_condition_once() -> void:
	# The loop body never runs with a zero budget, but the final post-loop
	# check must still give an already-satisfied condition a chance to pass.
	var met: bool = await _waiter.wait_condition(func() -> bool: return true, 0, POLL_MS)
	assert_bool(met).is_true()


# ── wait_for_node ────────────────────────────────────────────────────────

func test_wait_for_existing_node_succeeds() -> void:
	var result: Dictionary = await _waiter.wait_for_node(
		"group:waiter_probe", "exists", TIMEOUT_MS, POLL_MS
	)
	assert_bool(result.get("success", false)).is_true()
	assert_bool(result.get("found", false)).is_true()


func test_wait_for_absent_node_times_out() -> void:
	var result: Dictionary = await _waiter.wait_for_node(
		"group:no_such_group_at_all", "exists", TIMEOUT_MS, POLL_MS
	)
	assert_str(str(result.get("error_code", ""))).is_equal("timeout")
	assert_str(str(result.get("error", ""))).contains("did not appear")


func test_wait_for_node_removed_succeeds_when_absent() -> void:
	var result: Dictionary = await _waiter.wait_for_node(
		"group:no_such_group_at_all", "removed", TIMEOUT_MS, POLL_MS
	)
	assert_bool(result.get("success", false)).is_true()
	assert_bool(result.get("removed", false)).is_true()


func test_wait_for_node_removed_times_out_while_present() -> void:
	var result: Dictionary = await _waiter.wait_for_node(
		"group:waiter_probe", "removed", TIMEOUT_MS, POLL_MS
	)
	assert_str(str(result.get("error_code", ""))).is_equal("timeout")
	assert_str(str(result.get("error", ""))).contains("did not disappear")


func test_wait_for_visible_node_succeeds() -> void:
	var visible_node: Control = auto_free(Control.new())
	visible_node.name = "VisibleProbe"
	visible_node.visible = true
	visible_node.add_to_group(&"waiter_visible_probe")
	add_child(visible_node)

	var result: Dictionary = await _waiter.wait_for_node(
		"group:waiter_visible_probe", "visible", TIMEOUT_MS, POLL_MS
	)
	assert_bool(result.get("success", false)).is_true()
	assert_bool(result.get("visible", false)).is_true()


func test_wait_for_visible_times_out_on_hidden_node() -> void:
	var hidden: Control = auto_free(Control.new())
	hidden.name = "HiddenProbe"
	hidden.visible = false
	hidden.add_to_group(&"waiter_hidden_probe")
	add_child(hidden)

	var result: Dictionary = await _waiter.wait_for_node(
		"group:waiter_hidden_probe", "visible", TIMEOUT_MS, POLL_MS
	)
	assert_str(str(result.get("error_code", ""))).is_equal("timeout")
	assert_str(str(result.get("error", ""))).contains("did not become visible")


func test_wait_for_visible_times_out_when_ancestor_is_hidden() -> void:
	var menu: Control = auto_free(Control.new())
	menu.name = "HiddenMenu"
	menu.visible = false
	add_child(menu)
	var button: Control = Control.new()
	button.name = "MenuButton"
	button.visible = true
	button.add_to_group(&"waiter_nested_probe")
	menu.add_child(button)

	var result: Dictionary = await _waiter.wait_for_node(
		"group:waiter_nested_probe", "visible", TIMEOUT_MS, POLL_MS
	)
	assert_str(str(result.get("error_code", ""))).is_equal("timeout")

	menu.visible = true
	result = await _waiter.wait_for_node(
		"group:waiter_nested_probe", "visible", TIMEOUT_MS, POLL_MS
	)
	assert_bool(result.get("success", false)).is_true()


# ── evaluate_property_condition ──────────────────────────────────────────

func test_equals_operator_matches() -> void:
	assert_bool(
		_waiter.evaluate_property_condition(_probe, "name", "equals", "WaitProbe")
	).is_true()


func test_equals_operator_rejects_mismatch() -> void:
	assert_bool(
		_waiter.evaluate_property_condition(_probe, "name", "equals", "SomethingElse")
	).is_false()


func test_not_equals_operator() -> void:
	assert_bool(
		_waiter.evaluate_property_condition(_probe, "name", "not_equals", "Other")
	).is_true()


func test_exists_operator() -> void:
	assert_bool(
		_waiter.evaluate_property_condition(_probe, "name", "exists", null)
	).is_true()


func test_contains_operator_on_string() -> void:
	var holder: Node = auto_free(Node.new())
	holder.set_script(preload("res://scripts/property_target.gd"))
	add_child(holder)
	assert_bool(
		_waiter.evaluate_property_condition(holder, "text_prop", "contains", "init")
	).is_true()


func test_contains_operator_on_stringname_property() -> void:
	# Node.name is a StringName, not a String; "contains" must still match it.
	assert_bool(
		_waiter.evaluate_property_condition(_probe, "name", "contains", "Probe")
	).is_true()


func test_contains_operator_rejects_absent_substring() -> void:
	var holder: Node = auto_free(Node.new())
	holder.set_script(preload("res://scripts/property_target.gd"))
	add_child(holder)
	assert_bool(
		_waiter.evaluate_property_condition(holder, "text_prop", "contains", "nope")
	).is_false()


func test_greater_than_operator_on_numbers() -> void:
	var node: Node2D = auto_free(Node2D.new())
	node.name = "NumericProbe"
	node.rotation = 1.0
	add_child(node)

	assert_bool(
		_waiter.evaluate_property_condition(node, "rotation", "greater_than", 0.5)
	).is_true()
	assert_bool(
		_waiter.evaluate_property_condition(node, "rotation", "less_than", 0.5)
	).is_false()


func _float32(v: float) -> float:
	return PackedFloat32Array([v])[0]


func test_equals_tolerates_float32_widening() -> void:
	# float32(0.1) reads back as 0.10000000149011612; exact == never matches 0.1.
	var node: Node2D = auto_free(Node2D.new())
	node.rotation = _float32(0.1)
	add_child(node)
	assert_bool(_waiter.evaluate_property_condition(node, "rotation", "equals", 0.1)).is_true()
	assert_bool(_waiter.evaluate_property_condition(node, "rotation", "not_equals", 0.1)).is_false()


func test_equals_tolerance_zero_is_exact() -> void:
	var node: Node2D = auto_free(Node2D.new())
	node.rotation = _float32(0.1)
	add_child(node)
	assert_bool(_waiter.evaluate_property_condition(node, "rotation", "equals", 0.1, 0.0)).is_false()
	assert_bool(_waiter.evaluate_property_condition(node, "rotation", "not_equals", 0.1, 0.0)).is_true()


func test_equals_rejects_difference_beyond_tolerance() -> void:
	var node: Node2D = auto_free(Node2D.new())
	node.rotation = 0.5
	add_child(node)
	assert_bool(_waiter.evaluate_property_condition(node, "rotation", "equals", 0.51)).is_false()
	assert_bool(_waiter.evaluate_property_condition(node, "rotation", "equals", 0.51, 0.05)).is_true()


func test_tolerance_is_relative_for_large_magnitudes() -> void:
	var node: Node2D = auto_free(Node2D.new())
	node.rotation = 1000.0
	add_child(node)
	# |a-b| = 9 <= 0.01 * max(1, 1000, 1009)
	assert_bool(_waiter.evaluate_property_condition(node, "rotation", "equals", 1009.0, 0.01)).is_true()
	assert_bool(_waiter.evaluate_property_condition(node, "rotation", "equals", 1009.0, 0.001)).is_false()


func test_ordering_excludes_values_equal_under_tolerance() -> void:
	var node: Node2D = auto_free(Node2D.new())
	node.rotation = _float32(0.1)
	add_child(node)
	assert_bool(_waiter.evaluate_property_condition(node, "rotation", "greater_than", 0.1)).is_false()
	assert_bool(_waiter.evaluate_property_condition(node, "rotation", "less_than", 0.1)).is_false()
	assert_bool(_waiter.evaluate_property_condition(node, "rotation", "greater_than", 0.1, 0.0)).is_true()


func test_ordering_still_passes_for_clear_differences() -> void:
	var node: Node2D = auto_free(Node2D.new())
	node.rotation = 1.0
	add_child(node)
	assert_bool(_waiter.evaluate_property_condition(node, "rotation", "greater_than", 0.5)).is_true()
	assert_bool(_waiter.evaluate_property_condition(node, "rotation", "less_than", 2.0)).is_true()


func test_wait_for_property_matches_float32_value_with_default_tolerance() -> void:
	var node: Node2D = auto_free(Node2D.new())
	node.name = "FloatWaitProbe"
	node.rotation = _float32(0.1)
	add_child(node)
	var result: Dictionary = await _waiter.wait_for_property(
		"name:FloatWaitProbe", "rotation", "equals", 0.1, TIMEOUT_MS, POLL_MS
	)
	assert_bool(result.get("success", false)).is_true()


func test_wait_for_property_tolerance_zero_requires_exact_match() -> void:
	var node: Node2D = auto_free(Node2D.new())
	node.name = "ExactWaitProbe"
	node.rotation = _float32(0.1)
	add_child(node)
	var result: Dictionary = await _waiter.wait_for_property(
		"name:ExactWaitProbe", "rotation", "equals", 0.1, TIMEOUT_MS, POLL_MS, 0.0
	)
	assert_bool(result.get("success", false)).is_false()


func test_property_condition_never_invokes_get_method() -> void:
	var node: Node = auto_free(Node.new())
	node.set_script(preload("res://scripts/getter_side_effect.gd"))
	add_child(node)
	assert_bool(_waiter.evaluate_property_condition(node, "boom", "exists", null)).is_false()
	var calls: int = node.get("boom_calls")
	assert_int(calls).is_equal(0)


func test_property_condition_reads_nested_path() -> void:
	var node: Node2D = auto_free(Node2D.new())
	node.position = Vector2(3.0, 4.0)
	add_child(node)
	assert_bool(_waiter.evaluate_property_condition(node, "position.x", "equals", 3.0)).is_true()


func test_property_condition_reads_meta() -> void:
	_probe.set_meta(&"score", 7)
	assert_bool(_waiter.evaluate_property_condition(_probe, "score", "equals", 7)).is_true()


func test_wait_for_property_rejects_private_and_script() -> void:
	var holder: Node = auto_free(Node.new())
	holder.set_script(preload("res://scripts/property_target.gd"))
	holder.add_to_group(&"waiter_private_probe")
	add_child(holder)
	for property: String in ["_private_prop", "script", "script._x"]:
		var result: Dictionary = await _waiter.wait_for_property(
			"group:waiter_private_probe", property, "exists", null, TIMEOUT_MS, POLL_MS
		)
		assert_str(str(result.get("error_code", ""))) \
			.override_failure_message("expected '%s' to be refused" % property) \
			.is_equal("not_supported")


func test_numeric_operator_on_non_numeric_value_is_false() -> void:
	# A String property can't be ordered against a number.
	assert_bool(
		_waiter.evaluate_property_condition(_probe, "name", "greater_than", 5)
	).is_false()


# ── stringified expected_value (godot-stagehand-wait-for-property-stringified-expected-60sz) ──

func test_equals_matches_stringified_int() -> void:
	var holder: Node = auto_free(Node.new())
	holder.set_script(preload("res://scripts/property_target.gd"))
	add_child(holder)
	assert_bool(
		_waiter.evaluate_property_condition(holder, "count_prop", "equals", "5")
	).is_true()


func test_equals_matches_stringified_float() -> void:
	var holder: Node = auto_free(Node.new())
	holder.set_script(preload("res://scripts/property_target.gd"))
	add_child(holder)
	assert_bool(
		_waiter.evaluate_property_condition(holder, "ratio_prop", "equals", "0.5")
	).is_true()


func test_equals_matches_stringified_bool() -> void:
	var holder: Node = auto_free(Node.new())
	holder.set_script(preload("res://scripts/property_target.gd"))
	add_child(holder)
	assert_bool(
		_waiter.evaluate_property_condition(holder, "flag_prop", "equals", "true")
	).is_true()


func test_not_equals_rejects_stringified_int_match() -> void:
	var holder: Node = auto_free(Node.new())
	holder.set_script(preload("res://scripts/property_target.gd"))
	add_child(holder)
	assert_bool(
		_waiter.evaluate_property_condition(holder, "count_prop", "not_equals", "5")
	).is_false()


func test_equals_on_string_property_still_compares_stringified_expected_literally() -> void:
	# Target-awareness: text_prop = "50" must stay two characters, so "equals"
	# with a stringified expected_value of "50" must not be JSON-decoded away.
	var holder: Node = auto_free(Node.new())
	holder.set_script(preload("res://scripts/property_target.gd"))
	holder.set("text_prop", "50")
	add_child(holder)
	assert_bool(
		_waiter.evaluate_property_condition(holder, "text_prop", "equals", "50")
	).is_true()


func test_greater_than_matches_stringified_number() -> void:
	var holder: Node = auto_free(Node.new())
	holder.set_script(preload("res://scripts/property_target.gd"))
	add_child(holder)
	assert_bool(
		_waiter.evaluate_property_condition(holder, "count_prop", "greater_than", "3")
	).is_true()


func test_less_than_matches_stringified_number() -> void:
	var holder: Node = auto_free(Node.new())
	holder.set_script(preload("res://scripts/property_target.gd"))
	add_child(holder)
	assert_bool(
		_waiter.evaluate_property_condition(holder, "count_prop", "less_than", "10")
	).is_true()


func test_unknown_property_is_false() -> void:
	assert_bool(
		_waiter.evaluate_property_condition(_probe, "no_such_property", "exists", null)
	).is_false()


func test_unknown_operator_is_false() -> void:
	assert_bool(
		_waiter.evaluate_property_condition(_probe, "name", "no_such_operator", "x")
	).is_false()


# ── wait_for_property ────────────────────────────────────────────────────

func test_wait_for_satisfied_property_succeeds() -> void:
	var result: Dictionary = await _waiter.wait_for_property(
		"group:waiter_probe", "name", "equals", "WaitProbe", TIMEOUT_MS, POLL_MS
	)
	assert_bool(result.get("success", false)).is_true()
	assert_bool(result.get("met_condition", false)).is_true()


func test_wait_for_unsatisfiable_property_times_out() -> void:
	var result: Dictionary = await _waiter.wait_for_property(
		"group:waiter_probe", "name", "equals", "NeverThisName", TIMEOUT_MS, POLL_MS
	)
	assert_str(str(result.get("error_code", ""))).is_equal("timeout")
	assert_str(str(result.get("error", ""))).contains("not met before timeout")


func test_wait_for_property_on_missing_node_times_out() -> void:
	var result: Dictionary = await _waiter.wait_for_property(
		"group:no_such_group_at_all", "name", "exists", null, TIMEOUT_MS, POLL_MS
	)
	assert_str(str(result.get("error_code", ""))).is_equal("timeout")


# ── wait_for_signal ──────────────────────────────────────────────────────

func test_wait_for_signal_receives_emission() -> void:
	# Node emits `renamed` when its name changes; fire it just after the wait
	# starts so the one-shot connection is already in place.
	var emitter: Node = _probe
	get_tree().create_timer(0.02).timeout.connect(
		func() -> void: emitter.name = "WaitProbeRenamed"
	)

	var result: Dictionary = await _waiter.wait_for_signal(
		"group:waiter_probe", "renamed", 2000
	)
	assert_bool(result.get("received", false)).is_true()
	assert_int(result.get("elapsed_ms", -1)).is_greater_equal(0)


func test_wait_for_signal_that_never_fires_times_out() -> void:
	var result: Dictionary = await _waiter.wait_for_signal(
		"group:waiter_probe", "renamed", TIMEOUT_MS
	)
	assert_str(str(result.get("error_code", ""))).is_equal("timeout")
	assert_str(str(result.get("error", ""))).contains("was not emitted before timeout")


func test_wait_for_signal_on_missing_node_returns_error() -> void:
	var result: Dictionary = await _waiter.wait_for_signal(
		"group:no_such_group_at_all", "renamed", TIMEOUT_MS
	)
	assert_str(str(result.get("error_code", ""))).is_equal("node_not_found")
	assert_str(str(result.get("error", ""))).contains("Node not found")


func test_wait_for_unknown_signal_returns_error() -> void:
	var result: Dictionary = await _waiter.wait_for_signal(
		"group:waiter_probe", "no_such_signal", TIMEOUT_MS
	)
	assert_str(str(result.get("error_code", ""))).is_equal("invalid_params")
	assert_str(str(result.get("error", ""))).contains("not found on node")


func _emit_soon(emitter: Node, signal_name: StringName, args: Array) -> void:
	get_tree().create_timer(0.02).timeout.connect(
		func() -> void: emitter.callv(&"emit_signal", [signal_name] + args)
	)


func _wait_on_emitter(signal_name: String) -> Dictionary:
	return await _waiter.wait_for_signal("group:waiter_emitter", signal_name, 2000)


func _add_emitter() -> Node:
	var emitter: Node = auto_free(SignalEmitter.new())
	emitter.add_to_group(&"waiter_emitter")
	add_child(emitter)
	return emitter


func test_wait_for_signal_preserves_null_args() -> void:
	var emitter: Node = _add_emitter()
	_emit_soon(emitter, &"two_args", [null, 3])
	var result: Dictionary = await _wait_on_emitter("two_args")
	assert_bool(result.get("received", false)).is_true()
	var args: Array = result.get("args", [])
	assert_int(args.size()).is_equal(2)
	assert_object(args[0]).is_null()
	assert_int(args[1]).is_equal(3)


func test_wait_for_signal_keeps_trailing_null() -> void:
	var emitter: Node = _add_emitter()
	_emit_soon(emitter, &"trailing_null", [1, null])
	var result: Dictionary = await _wait_on_emitter("trailing_null")
	var args: Array = result.get("args", [])
	assert_int(args.size()).is_equal(2)


func test_wait_for_signal_with_no_args_reports_empty_args() -> void:
	var emitter: Node = _add_emitter()
	_emit_soon(emitter, &"no_args", [])
	var result: Dictionary = await _wait_on_emitter("no_args")
	assert_bool(result.get("received", false)).is_true()
	var args: Array = result.get("args", [])
	assert_int(args.size()).is_equal(0)


func test_wait_for_signal_captures_eight_args() -> void:
	var emitter: Node = _add_emitter()
	_emit_soon(emitter, &"eight_args", [1, 2, 3, 4, 5, 6, 7, 8])
	var result: Dictionary = await _wait_on_emitter("eight_args")
	var args: Array = result.get("args", [])
	assert_int(args.size()).is_equal(8)
	assert_int(args[7]).is_equal(8)


func test_wait_for_signal_with_more_than_eight_args_errors_clearly() -> void:
	_add_emitter()
	var result: Dictionary = await _wait_on_emitter("nine_args")
	assert_str(str(result.get("error_code", ""))).is_equal("invalid_params")
	assert_str(str(result.get("error", ""))).contains("9 arguments")


# ── Engine.time_scale = 0 ────────────────────────────────────────────────
# Deadlines are wall-clock, so the poll sleeps must be too: a scaled timer at
# time_scale 0 never wakes the loop to notice its own deadline.

func after_test() -> void:
	Engine.time_scale = 1.0


func test_wait_for_node_times_out_when_time_scale_is_zero() -> void:
	Engine.time_scale = 0.0
	var state: Dictionary = {"done": false}
	var run: Callable = func() -> void:
		await _waiter.wait_for_node("group:no_such_group_at_all", "exists", TIMEOUT_MS, POLL_MS)
		state["done"] = true
	run.call()
	await get_tree().create_timer(0.6, true, false, true).timeout
	assert_bool(state["done"]).is_true()


func test_wait_for_signal_times_out_when_time_scale_is_zero() -> void:
	Engine.time_scale = 0.0
	var state: Dictionary = {"done": false}
	var run: Callable = func() -> void:
		await _waiter.wait_for_signal("group:waiter_probe", "renamed", TIMEOUT_MS)
		state["done"] = true
	run.call()
	await get_tree().create_timer(0.6, true, false, true).timeout
	assert_bool(state["done"]).is_true()
