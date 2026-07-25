# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for StagehandServer._dispatch_and_respond's handling of a handler
## that aborts with a GDScript runtime error mid-execution
## (godot-stagehand-addon-handler-abort-response-imt;
## docs/audits/2026-07-08-implementation-audit.md finding S8).
##
## Confirmed via instrumentation before writing this test (not assumed): a
## GDScript runtime error inside a Dictionary-returning function does not
## hang the awaiting coroutine forever. Godot 4.6.2 aborts only the erroring
## function and resumes the awaiter with that function's declared-type
## default value — `{}` for every handler registered in stagehand_server.gd,
## since each one declares `-> Dictionary` and none legitimately returns an
## empty dict on its success path or its own defined `{"error": ...}` path.
## Before this fix, that bogus empty `{}` was forwarded to the client inside
## a normal JSON-RPC *success* response, silently masking the failure instead
## of surfacing it — the client had no way to distinguish "handler crashed"
## from "handler genuinely returned nothing."

const TestableStagehandServer := preload("res://test/fixtures/testable_stagehand_server.gd")
const JSON_RPC := preload("res://addons/stagehand/protocol/json_rpc.gd")

var _server: TestableStagehandServer


func before_test() -> void:
	_server = TestableStagehandServer.new()
	add_child(_server)
	auto_free(_server)
	_server._router = StagehandCommandRouter.new()


## Parse a serialized JSON-RPC frame back into a Dictionary for assertions.
func _decode(text: String) -> Dictionary:
	var json: JSON = JSON.new()
	var err: Error = json.parse(text)
	assert_int(err).is_equal(OK)
	var data: Variant = json.data
	assert_bool(data is Dictionary).is_true()
	return data


func test_handler_runtime_error_sends_json_rpc_error_not_bogus_success() -> void:
	_server._router.register("boom", func(_p: Variant) -> Dictionary:
		var missing: Node = null
		# Deliberately trips a GDScript runtime error (null-instance call) to
		# abort this Callable partway through, the same way a bad evaluate/
		# call_method/set_property does in production.
		var _unreachable: Variant = missing.get_name()
		return {"should": "never be reached"}
	)

	# The dispatch is expected to trigger exactly this runtime error — assert
	# on it via GdUnit4's error-monitor API rather than letting it fall
	# through as an unhandled per-test error in the JUnit report. No `await`
	# here: assert_error()'s static return type (the abstract
	# GdUnitGodotErrorAssert) declares is_runtime_error with no internal
	# await, so the compiler (correctly, per strict mode) flags an outer
	# await as unnecessary even though the concrete implementation awaits;
	# the callable itself only ever suspends synchronously in this scenario
	# (a Callable with no internal `await` of its own).
	assert_error(func() -> void:
		await _server._dispatch_and_respond(1, 42, "boom", {})
	).is_runtime_error("Cannot call method 'get_name' on a null value.")

	assert_int(_server.sent_frames.size()).is_equal(1)
	var sent_text: String = _server.sent_frames[0]["text"]
	var envelope: Dictionary = _decode(sent_text)
	assert_bool(envelope.has("error")).is_true()
	assert_bool(envelope.has("result")).is_false()
	var error_obj: Dictionary = envelope.get("error", {})
	var error_code: float = error_obj.get("code", 0)
	assert_int(int(error_code)).is_equal(JSON_RPC.INTERNAL_ERROR)
	var response_id: float = envelope.get("id", -1.0)
	assert_float(response_id).is_equal_approx(42.0, 0.001)


func test_handler_success_still_sends_normal_result() -> void:
	_server._router.register("ok", func(_p: Variant) -> Dictionary:
		return {"value": 1}
	)

	await _server._dispatch_and_respond(1, 7, "ok", {})

	assert_int(_server.sent_frames.size()).is_equal(1)
	var sent_text: String = _server.sent_frames[0]["text"]
	var envelope: Dictionary = _decode(sent_text)
	assert_bool(envelope.has("result")).is_true()
	assert_bool(envelope.has("error")).is_false()
	# JSON has a single number type, so the int 1 the handler returned round-trips
	# through the wire as a float — assert against that, not the original int.
	assert_dict(envelope["result"]).is_equal({"value": 1.0})


func test_notification_with_no_id_sends_nothing_even_on_abort() -> void:
	_server._router.register("boom", func(_p: Variant) -> Dictionary:
		var missing: Node = null
		var _unreachable: Variant = missing.get_name()
		return {}
	)

	assert_error(func() -> void:
		await _server._dispatch_and_respond(1, null, "boom", {})
	).is_runtime_error("Cannot call method 'get_name' on a null value.")

	assert_int(_server.sent_frames.size()).is_equal(0)


func test_handler_error_envelope_becomes_a_json_rpc_error_not_a_success() -> void:
	# The core of godot-stagehand-vv2.8: before this, a handler failure was
	# forwarded inside a JSON-RPC *success* response with an "error" key, which
	# an MCP client could not distinguish from a successful call.
	_server._router.register("lookup", func(_p: Variant) -> Dictionary:
		return StagehandErrors.node_not_found("name:Ghost")
	)

	await _server._dispatch_and_respond(1, 9, "lookup", {"selector": "name:Ghost"})

	assert_int(_server.sent_frames.size()).is_equal(1)
	var sent_text: String = _server.sent_frames[0]["text"]
	var envelope: Dictionary = _decode(sent_text)
	assert_bool(envelope.has("result")).is_false()

	var error_obj: Dictionary = envelope.get("error", {})
	var error_code: float = error_obj.get("code", 0)
	assert_int(int(error_code)).is_equal(StagehandErrors.RPC_TARGET_NOT_FOUND)
	var data: Dictionary = error_obj.get("data", {})
	assert_that(data.get("error_code")).is_equal("node_not_found")
	assert_that(data.get("method")).is_equal("lookup")
	# The selector is echoed from the request params so a client can attribute
	# the failure to a target without re-parsing what it sent.
	assert_that(data.get("selector")).is_equal("name:Ghost")


func test_handler_error_without_a_selector_param_omits_it() -> void:
	_server._router.register("capture", func(_p: Variant) -> Dictionary:
		return StagehandErrors.make(StagehandErrors.TIMEOUT, "Gave up")
	)

	await _server._dispatch_and_respond(1, 11, "capture", {})

	var sent_text: String = _server.sent_frames[0]["text"]
	var envelope: Dictionary = _decode(sent_text)
	var error_obj: Dictionary = envelope.get("error", {})
	var error_code: float = error_obj.get("code", 0)
	assert_int(int(error_code)).is_equal(StagehandErrors.RPC_TIMEOUT)
	var data: Dictionary = error_obj.get("data", {})
	assert_bool(data.has("selector")).is_false()


func test_notification_with_no_id_sends_nothing_on_a_handler_error() -> void:
	_server._router.register("lookup", func(_p: Variant) -> Dictionary:
		return StagehandErrors.node_not_found("name:Ghost")
	)

	await _server._dispatch_and_respond(1, null, "lookup", {"selector": "name:Ghost"})

	assert_int(_server.sent_frames.size()).is_equal(0)


@warning_ignore_restore("return_value_discarded")
