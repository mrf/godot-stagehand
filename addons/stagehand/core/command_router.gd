class_name StagehandCommandRouter
extends RefCounted
## Routes incoming JSON-RPC method names to handler callables.

## Preloaded rather than referenced by its `class_name` so it resolves in a
## headless launch with an empty global class cache — see the rationale block in
## autoload/stagehand_server.gd.
const ERRORS := preload("res://addons/stagehand/core/errors.gd")

## [method dispatch_checked] discriminators.
const OUTCOME_OK: String = "ok"
const OUTCOME_ERROR: String = "error"


var _handlers: Dictionary = {}  # String -> Callable


## Register a handler callable for a JSON-RPC method name.
func register(method: String, handler: Callable) -> void:
	_handlers[method] = handler


## Remove a registered handler.
func unregister(method: String) -> void:
	var _erased: bool = _handlers.erase(method)


## Check whether a handler is registered for the given method.
func has_handler(method: String) -> bool:
	return _handlers.has(method)


## Call the registered handler for [param method] with [param params].
## Returns the handler's return value. Caller must check [method has_handler] first.
##
## NOTE: dispatch is synchronous and does NOT await coroutine handlers — a
## handler that suspends on `await` (e.g. screenshot) would return null here.
## For handlers that may be coroutines, fetch the Callable via [method get_handler]
## and `await handler.call(params)` at the call site (see StagehandServer).
func dispatch(method: String, params: Variant) -> Variant:
	var handler: Callable = _handlers[method]
	return handler.call(params)


## Return the registered handler Callable for [param method], or an empty
## Callable if none is registered. Lets callers `await` coroutine handlers
## (such as screenshot) directly, which dispatch cannot do without becoming a
## coroutine itself. Check [method has_handler] first.
func get_handler(method: String) -> Callable:
	if not _handlers.has(method):
		return Callable()
	var handler: Callable = _handlers[method]
	return handler


## Await the handler for [param method] and classify what came back, so the
## transport never has to guess whether a handler succeeded. Caller must check
## [method has_handler] first — an unregistered method is a protocol-level fault
## (JSON-RPC -32601) that the transport reports before it ever reaches here.
##
## Returns one of:
## [codeblock]
## {"outcome": OUTCOME_OK,    "result": Variant}
## {"outcome": OUTCOME_ERROR, "error": <canonical envelope, see core/errors.gd>}
## [/codeblock]
##
## Two failure modes collapse into OUTCOME_ERROR:
##   1. The handler returned a canonical failure envelope (see [method
##      StagehandErrors.is_error]) — the normal "this request cannot be
##      fulfilled" path.
##   2. The handler aborted on an unhandled GDScript runtime error. GDScript has
##      no try/catch, so an abort does not unwind as an exception; confirmed by
##      instrumented reproduction against Godot 4.6.2, it aborts only the
##      erroring function and resumes the awaiter with that function's
##      declared-type default value. Every registered handler declares
##      `-> Dictionary` and always returns a non-empty Dictionary on both its
##      success and its defined-failure paths, so an exactly-empty (or
##      non-Dictionary) result is an unambiguous abort signal. It is reported as
##      an [constant StagehandErrors.INTERNAL] envelope rather than forwarded as
##      a bogus success (docs/audits/2026-07-08-implementation-audit.md
##      finding S8).
##
## This is a coroutine so it can await handlers that suspend (screenshot,
## input_text); [method dispatch] cannot, which is why both exist.
func dispatch_checked(method: String, params: Variant) -> Dictionary:
	var handler: Callable = _handlers[method]
	var result: Variant = await handler.call(params)

	if _handler_aborted(result):
		return {
			"outcome": OUTCOME_ERROR,
			"error": ERRORS.make(
				ERRORS.INTERNAL,
				"Handler '%s' failed with an internal error" % method,
				{
					"next_action": "Check the Godot process output for the GDScript runtime error this method raised.",
				}
			),
		}

	if ERRORS.is_error(result):
		var envelope: Dictionary = result
		return {"outcome": OUTCOME_ERROR, "error": envelope}

	return {"outcome": OUTCOME_OK, "result": result}


## Whether a handler's returned value indicates that the handler Callable
## aborted partway through execution rather than completing normally. See
## [method dispatch_checked] for the mechanism this relies on.
static func _handler_aborted(result: Variant) -> bool:
	if result is not Dictionary:
		return true
	var dict: Dictionary = result
	return dict.is_empty()


## Return all registered method names.
func get_methods() -> PackedStringArray:
	return PackedStringArray(_handlers.keys())
