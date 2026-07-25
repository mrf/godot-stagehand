# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for StagehandErrors — the canonical Godot Wire Protocol failure
## envelope and its mapping onto JSON-RPC 2.0 error codes
## (godot-stagehand-vv2.8; docs/error-model.md).


func test_make_omits_empty_details() -> void:
	var envelope: Dictionary = StagehandErrors.make(
		StagehandErrors.NODE_NOT_FOUND, "Node not found"
	)
	assert_that(envelope.get("error")).is_equal("Node not found")
	assert_that(envelope.get("error_code")).is_equal("node_not_found")
	assert_bool(envelope.has("details")).is_false()


func test_make_carries_details() -> void:
	var envelope: Dictionary = StagehandErrors.make(
		StagehandErrors.TIMEOUT, "Gave up", {"timeout_ms": 500}
	)
	var details: Dictionary = StagehandErrors.details_of(envelope)
	assert_int(details.get("timeout_ms", 0)).is_equal(500)


func test_node_not_found_carries_selector_and_next_action() -> void:
	var envelope: Dictionary = StagehandErrors.node_not_found("name:Ghost")
	assert_that(StagehandErrors.code_of(envelope)).is_equal(StagehandErrors.NODE_NOT_FOUND)
	assert_str(StagehandErrors.message_of(envelope)).contains("name:Ghost")
	var details: Dictionary = StagehandErrors.details_of(envelope)
	assert_that(details.get("selector")).is_equal("name:Ghost")
	assert_str(str(details.get("next_action", ""))).is_not_empty()


func test_missing_param_names_the_parameter() -> void:
	var envelope: Dictionary = StagehandErrors.missing_param("selector")
	assert_that(StagehandErrors.code_of(envelope)).is_equal(StagehandErrors.INVALID_PARAMS)
	assert_str(StagehandErrors.message_of(envelope)).contains("selector")


func test_is_error_recognises_the_envelope() -> void:
	assert_bool(StagehandErrors.is_error(
		StagehandErrors.make(StagehandErrors.INTERNAL, "boom")
	)).is_true()


func test_is_error_rejects_success_results() -> void:
	assert_bool(StagehandErrors.is_error({"success": true})).is_false()
	assert_bool(StagehandErrors.is_error({})).is_false()
	assert_bool(StagehandErrors.is_error(null)).is_false()
	assert_bool(StagehandErrors.is_error("not a dictionary")).is_false()


func test_is_error_rejects_an_empty_error_string() -> void:
	# A result that merely *has* the key, with nothing in it, is not a failure —
	# treating it as one would turn a legitimate result into a reported error.
	assert_bool(StagehandErrors.is_error({"error": ""})).is_false()


func test_is_error_accepts_a_legacy_envelope_without_a_code() -> void:
	# An addon predating this module reported `{"error": "..."}` with no kind.
	# It must still classify as a failure, defaulting to the internal kind.
	var legacy: Dictionary = {"error": "something went wrong"}
	assert_bool(StagehandErrors.is_error(legacy)).is_true()
	assert_that(StagehandErrors.code_of(legacy)).is_equal(StagehandErrors.INTERNAL)


func test_json_rpc_code_maps_parameter_faults_to_invalid_params() -> void:
	for code: String in [
		StagehandErrors.INVALID_PARAMS,
		StagehandErrors.INVALID_SELECTOR,
		StagehandErrors.INVALID_VALUE,
	]:
		assert_int(StagehandErrors.json_rpc_code(code)).is_equal(-32602)


func test_json_rpc_code_maps_absent_targets_to_target_not_found() -> void:
	for code: String in [
		StagehandErrors.NODE_NOT_FOUND,
		StagehandErrors.PROPERTY_NOT_FOUND,
		StagehandErrors.METHOD_NOT_FOUND,
		StagehandErrors.SCENE_NOT_FOUND,
	]:
		assert_int(StagehandErrors.json_rpc_code(code)).is_equal(
			StagehandErrors.RPC_TARGET_NOT_FOUND
		)


func test_json_rpc_code_maps_timeout_and_internal_distinctly() -> void:
	assert_int(StagehandErrors.json_rpc_code(StagehandErrors.TIMEOUT)).is_equal(
		StagehandErrors.RPC_TIMEOUT
	)
	assert_int(StagehandErrors.json_rpc_code(StagehandErrors.INTERNAL)).is_equal(-32603)


func test_json_rpc_code_falls_back_to_handler_error_for_unknown_kinds() -> void:
	# An unrecognised kind must never land on a reserved JSON-RPC code, or a
	# future addon-side kind would masquerade as a protocol-level fault.
	assert_int(StagehandErrors.json_rpc_code("some_future_kind")).is_equal(
		StagehandErrors.RPC_HANDLER_ERROR
	)
	assert_int(StagehandErrors.json_rpc_code(StagehandErrors.NOT_SUPPORTED)).is_equal(
		StagehandErrors.RPC_HANDLER_ERROR
	)


func test_renderer_unavailable_is_distinct_from_timeout() -> void:
	# A headless/GPU-less session deterministically cannot render, and a caller
	# skips a visual check on that while still failing a real capture timeout —
	# so the two kinds must not collapse onto one code.
	assert_str(StagehandErrors.RENDERER_UNAVAILABLE).is_not_equal(StagehandErrors.TIMEOUT)
	assert_int(StagehandErrors.json_rpc_code(StagehandErrors.RENDERER_UNAVAILABLE)).is_not_equal(
		StagehandErrors.json_rpc_code(StagehandErrors.TIMEOUT)
	)


@warning_ignore_restore("return_value_discarded")
