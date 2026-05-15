extends GdUnitTestSuite
## Tests for StagehandCommandRouter — JSON-RPC method dispatch table.


var _router: StagehandCommandRouter


func before_each() -> void:
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
	var result := _router.dispatch("echo", {"msg": "hello"})
	assert_that(result).is_equal({"msg": "hello"})


func test_dispatch_passes_params_to_handler() -> void:
	var received: Variant = null
	_router.register("capture", func(p: Variant) -> Variant:
		received = p
		return null
	)
	_router.dispatch("capture", [1, 2, 3])
	assert_that(received).is_equal([1, 2, 3])


func test_dispatch_null_params() -> void:
	_router.register("noop", func(_p: Variant) -> Variant: return "done")
	assert_that(_router.dispatch("noop", null)).is_equal("done")


func test_get_methods_empty() -> void:
	assert_that(_router.get_methods().size()).is_equal(0)


func test_get_methods_returns_registered_names() -> void:
	_router.register("alpha", func(_p: Variant) -> Variant: return null)
	_router.register("beta", func(_p: Variant) -> Variant: return null)
	var methods := _router.get_methods()
	assert_that(methods.size()).is_equal(2)
	assert_bool(methods.has("alpha")).is_true()
	assert_bool(methods.has("beta")).is_true()


func test_get_methods_excludes_unregistered() -> void:
	_router.register("keep", func(_p: Variant) -> Variant: return null)
	_router.register("remove", func(_p: Variant) -> Variant: return null)
	_router.unregister("remove")
	var methods := _router.get_methods()
	assert_that(methods.size()).is_equal(1)
	assert_bool(methods.has("keep")).is_true()
	assert_bool(methods.has("remove")).is_false()


func test_register_overwrites_existing_handler() -> void:
	_router.register("ping", func(_p: Variant) -> String: return "first")
	_router.register("ping", func(_p: Variant) -> String: return "second")
	assert_that(_router.dispatch("ping", null)).is_equal("second")


func test_get_methods_returns_packed_string_array() -> void:
	_router.register("m", func(_p: Variant) -> Variant: return null)
	assert_bool(_router.get_methods() is PackedStringArray).is_true()
