# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for StagehandCommandRouter — JSON-RPC method dispatch table.


var _router: StagehandCommandRouter


func before_test() -> void:
	_router = StagehandCommandRouter.new()


func test_has_handler_false_when_empty() -> void:
	assert_bool(_router.has_handler("ping")).is_false()


func test_register_makes_handler_visible() -> void:
	_router.register("ping", func(_p: Variant) -> String: return "pong")
	assert_bool(_router.has_handler("ping")).is_true()


func test_register_multiple_handlers() -> void:
	_router.register("foo", func(_p: Variant) -> Variant: return null)
	_router.register("bar", func(_p: Variant) -> Variant: return null)
	assert_bool(_router.has_handler("foo")).is_true()
	assert_bool(_router.has_handler("bar")).is_true()


func test_unregister_removes_handler() -> void:
	_router.register("ping", func(_p: Variant) -> String: return "pong")
	_router.unregister("ping")
	assert_bool(_router.has_handler("ping")).is_false()


func test_unregister_nonexistent_is_safe() -> void:
	# Should not crash when unregistering a method that was never registered.
	_router.unregister("ghost")
	assert_bool(_router.has_handler("ghost")).is_false()


func test_dispatch_returns_handler_result() -> void:
	_router.register("echo", func(p: Variant) -> Variant: return p)
	var result: Variant = _router.dispatch("echo", {"msg": "hello"})
	assert_that(result).is_equal({"msg": "hello"})


func test_dispatch_passes_params_to_handler() -> void:
	# GDScript lambdas capture by value, so the sink has to be a shared
	# reference (an Array) rather than a plain local.
	var received: Array = []
	_router.register("capture", func(p: Variant) -> Variant:
		received.append(p)
		return null
	)
	_router.dispatch("capture", [1, 2, 3])
	assert_int(received.size()).is_equal(1)
	assert_that(received[0]).is_equal([1, 2, 3])


func test_dispatch_null_params() -> void:
	_router.register("noop", func(_p: Variant) -> Variant: return "done")
	assert_that(_router.dispatch("noop", null)).is_equal("done")


## dispatch() is synchronous by contract: it cannot await a coroutine handler
## without becoming one itself. Callers that may register coroutines must go
## through get_handler() and await the Callable themselves — dispatching a
## coroutine directly raises "Trying to call an async function without await".
func test_get_handler_allows_awaiting_a_coroutine_handler() -> void:
	_router.register("delayed", _delayed_echo)
	var handler: Callable = _router.get_handler("delayed")
	var result: Variant = await handler.call({"status": "ok"})
	assert_that(result).is_equal({"status": "ok"})


func test_get_handler_returns_empty_callable_for_unknown_method() -> void:
	assert_bool(_router.get_handler("no_such_method").is_null()).is_true()


func test_get_methods_empty() -> void:
	assert_int(_router.get_methods().size()).is_equal(0)


func test_get_methods_returns_registered_names() -> void:
	_router.register("alpha", func(_p: Variant) -> Variant: return null)
	_router.register("beta", func(_p: Variant) -> Variant: return null)
	var methods: PackedStringArray = _router.get_methods()
	assert_int(methods.size()).is_equal(2)
	assert_bool(methods.has("alpha")).is_true()
	assert_bool(methods.has("beta")).is_true()


func test_get_methods_excludes_unregistered() -> void:
	_router.register("keep", func(_p: Variant) -> Variant: return null)
	_router.register("remove", func(_p: Variant) -> Variant: return null)
	_router.unregister("remove")
	var methods: PackedStringArray = _router.get_methods()
	assert_int(methods.size()).is_equal(1)
	assert_bool(methods.has("keep")).is_true()
	assert_bool(methods.has("remove")).is_false()


func test_register_overwrites_existing_handler() -> void:
	_router.register("ping", func(_p: Variant) -> String: return "first")
	_router.register("ping", func(_p: Variant) -> String: return "second")
	assert_that(_router.dispatch("ping", null)).is_equal("second")


func test_get_methods_returns_packed_string_array() -> void:
	_router.register("m", func(_p: Variant) -> Variant: return null)
	assert_bool(_router.get_methods() is PackedStringArray).is_true()


func _delayed_echo(p: Variant) -> Variant:
	await get_tree().process_frame
	return p


# ── dispatch_checked: handler failure classification (godot-stagehand-vv2.8) ──

func test_dispatch_checked_reports_success() -> void:
	_router.register("ok", func(_p: Variant) -> Dictionary: return {"value": 1})
	var outcome: Dictionary = await _router.dispatch_checked("ok", {})
	assert_that(outcome.get("outcome")).is_equal(StagehandCommandRouter.OUTCOME_OK)
	assert_dict(outcome.get("result", {})).is_equal({"value": 1})


func test_dispatch_checked_awaits_a_coroutine_handler() -> void:
	_router.register("slow", func(_p: Variant) -> Dictionary:
		await get_tree().process_frame
		return {"value": "late"}
	)
	var outcome: Dictionary = await _router.dispatch_checked("slow", {})
	assert_that(outcome.get("outcome")).is_equal(StagehandCommandRouter.OUTCOME_OK)
	var result: Dictionary = outcome.get("result", {})
	assert_that(result.get("value")).is_equal("late")


func test_dispatch_checked_surfaces_a_handler_error_envelope() -> void:
	_router.register("bad", func(_p: Variant) -> Dictionary:
		return StagehandErrors.node_not_found("name:Ghost")
	)
	var outcome: Dictionary = await _router.dispatch_checked("bad", {})
	assert_that(outcome.get("outcome")).is_equal(StagehandCommandRouter.OUTCOME_ERROR)
	var envelope: Dictionary = outcome.get("error", {})
	assert_that(StagehandErrors.code_of(envelope)).is_equal(StagehandErrors.NODE_NOT_FOUND)


func test_dispatch_checked_converts_an_aborted_handler_into_an_internal_error() -> void:
	_router.register("boom", func(_p: Variant) -> Dictionary:
		var missing: Node = null
		# Deliberately trips a GDScript runtime error to abort this Callable
		# partway through, exactly as a bad evaluate/call_method does in
		# production. GDScript has no try/catch: the abort resumes the awaiter
		# with the declared return type's default value, an empty Dictionary.
		var _unreachable: Variant = missing.get_name()
		return {"should": "never be reached"}
	)

	# A lambda captures by value, so an assignment to an outer local inside it
	# would not survive — share a one-element Array and mutate that instead.
	var captured: Array = [{}]
	# No `await` on assert_error itself — see the note in
	# test_stagehand_server_dispatch.gd for why the outer await is rejected.
	assert_error(func() -> void:
		captured[0] = await _router.dispatch_checked("boom", {})
	).is_runtime_error("Cannot call method 'get_name' on a null value.")

	var outcome: Dictionary = captured[0]
	assert_that(outcome.get("outcome")).is_equal(StagehandCommandRouter.OUTCOME_ERROR)
	var envelope: Dictionary = outcome.get("error", {})
	assert_that(StagehandErrors.code_of(envelope)).is_equal(StagehandErrors.INTERNAL)
	assert_str(StagehandErrors.message_of(envelope)).contains("boom")
