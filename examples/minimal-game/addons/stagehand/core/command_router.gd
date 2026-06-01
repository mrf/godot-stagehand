class_name StagehandCommandRouter
extends RefCounted
## Routes incoming JSON-RPC method names to handler callables.


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


## Return all registered method names.
func get_methods() -> PackedStringArray:
	return PackedStringArray(_handlers.keys())
