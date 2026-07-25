# GdUnit4 assertions are fluent and return self for chaining, so every
# unchained assert_*() trips return_value_discarded=2. Scoped, deliberate
# relaxation of that one warning; all other strict warnings stay errors.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for the Waiter node — condition polling, node-state waits, property
## conditions, signal waits, and timeout behavior.
##
## Timeouts are kept short (tens of ms) so the suite stays fast; each timeout
## assertion checks the returned contract rather than wall-clock precision,
## which is not reliable enough to assert on tightly.

const Waiter := preload("res://addons/stagehand/core/waiter.gd")

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
	assert_bool(result.get("success", true)).is_false()
	assert_bool(result.get("found", true)).is_false()
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
	assert_bool(result.get("success", true)).is_false()
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
	assert_bool(result.get("success", true)).is_false()
	assert_str(str(result.get("error", ""))).contains("did not become visible")


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
	# Uses a script-declared String property: Node.name is a StringName, which
	# the "contains" branch does not recognize as a string.
	var holder: Node = auto_free(Node.new())
	holder.set_script(preload("res://scripts/property_target.gd"))
	add_child(holder)
	assert_bool(
		_waiter.evaluate_property_condition(holder, "text_prop", "contains", "init")
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


func test_numeric_operator_on_non_numeric_value_is_false() -> void:
	# A String property can't be ordered against a number.
	assert_bool(
		_waiter.evaluate_property_condition(_probe, "name", "greater_than", 5)
	).is_false()


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
	assert_bool(result.get("success", true)).is_false()
	assert_bool(result.get("met_condition", true)).is_false()
	assert_str(str(result.get("error", ""))).contains("not met before timeout")


func test_wait_for_property_on_missing_node_times_out() -> void:
	var result: Dictionary = await _waiter.wait_for_property(
		"group:no_such_group_at_all", "name", "exists", null, TIMEOUT_MS, POLL_MS
	)
	assert_bool(result.get("success", true)).is_false()


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
	assert_bool(result.get("received", true)).is_false()
	assert_str(str(result.get("reason", ""))).is_equal("timeout")


func test_wait_for_signal_on_missing_node_returns_error() -> void:
	var result: Dictionary = await _waiter.wait_for_signal(
		"group:no_such_group_at_all", "renamed", TIMEOUT_MS
	)
	assert_bool(result.get("received", true)).is_false()
	assert_str(str(result.get("error", ""))).contains("Node not found")


func test_wait_for_unknown_signal_returns_error() -> void:
	var result: Dictionary = await _waiter.wait_for_signal(
		"group:waiter_probe", "no_such_signal", TIMEOUT_MS
	)
	assert_bool(result.get("received", true)).is_false()
	assert_str(str(result.get("error", ""))).contains("not found on node")
